package site

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type PublisherOptions struct {
	ContentRepo   string
	ContentBranch string
	ExpectedRepo  string
	StateDir      string
	BaseURL       string
	SiteTitle     string
	Language      string
	WebhookSecret string
	ListenAddr    string
	PollInterval  time.Duration
	EngineVersion string
	GitBinary     string
}

type publisher struct {
	options    PublisherOptions
	trigger    chan struct{}
	deliveries map[string]struct{}
	deliveryMu sync.Mutex
}

func RunPublisher(ctx context.Context, options PublisherOptions) error {
	p, err := newPublisher(options)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", p.health)
	mux.HandleFunc("/hooks/content", p.webhook)
	server := &http.Server{Addr: p.options.ListenAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		p.worker(ctx)
	}()
	p.enqueue()

	serverError := make(chan error, 1)
	go func() {
		log.Printf("publisher слушает %s", p.options.ListenAddr)
		serverError <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	<-workerDone
	return nil
}

func PublishOnce(ctx context.Context, options PublisherOptions) (PublishResult, error) {
	p, err := newPublisher(options)
	if err != nil {
		return PublishResult{}, err
	}
	return p.publish(ctx)
}

func newPublisher(options PublisherOptions) (*publisher, error) {
	if strings.TrimSpace(options.ContentRepo) == "" {
		return nil, errors.New("content repository is required")
	}
	if !strings.HasPrefix(options.ContentRepo, "git@github.com:") || !strings.HasSuffix(options.ContentRepo, ".git") {
		return nil, errors.New("content repository must use GitHub SSH URL")
	}
	if strings.TrimSpace(options.ExpectedRepo) == "" {
		return nil, errors.New("expected GitHub repository is required")
	}
	if strings.TrimSpace(options.WebhookSecret) == "" {
		return nil, errors.New("webhook secret is required")
	}
	if strings.TrimSpace(options.SiteTitle) == "" || strings.TrimSpace(options.Language) == "" {
		return nil, errors.New("site title and language are required")
	}
	if options.ContentBranch == "" {
		return nil, errors.New("content branch is required")
	}
	if strings.HasPrefix(options.ContentBranch, "-") {
		return nil, errors.New("content branch must not start with '-'")
	}
	if options.StateDir == "" {
		options.StateDir = "var"
	}
	if options.ListenAddr == "" {
		options.ListenAddr = "0.0.0.0:8080"
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 5 * time.Minute
	}
	if options.PollInterval < 5*time.Second {
		return nil, errors.New("poll interval must be at least 5 seconds")
	}
	if options.EngineVersion == "" {
		options.EngineVersion = "dev"
	}
	if options.GitBinary == "" {
		options.GitBinary = "git"
	}
	return &publisher{options: options, trigger: make(chan struct{}, 1), deliveries: make(map[string]struct{})}, nil
}

func (p *publisher) worker(ctx context.Context) {
	ticker := time.NewTicker(p.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.enqueue()
		case <-p.trigger:
			result, err := p.publish(ctx)
			if err != nil {
				log.Printf("публикация не выполнена; текущий релиз сохранён: %v", err)
				continue
			}
			if result.Release == "" {
				log.Printf("контент уже опубликован: %s", shortRevision(result.Revision))
			} else {
				mode := "инкрементально"
				if result.Full {
					mode = "полностью"
				}
				log.Printf("сайт опубликован %s: %d пост(ов), %d изменённых входов, commit %s", mode, result.Posts, result.Changed, shortRevision(result.Revision))
			}
		}
	}
}

func (p *publisher) enqueue() {
	select {
	case p.trigger <- struct{}{}:
	default:
	}
}

func (p *publisher) publish(ctx context.Context) (PublishResult, error) {
	if err := os.MkdirAll(p.options.StateDir, 0o755); err != nil {
		return PublishResult{}, err
	}
	lock, err := acquirePublishLock(filepath.Join(p.options.StateDir, "publish.lock"))
	if err != nil {
		return PublishResult{}, err
	}
	defer releasePublishLock(lock)

	checkout := filepath.Join(p.options.StateDir, "content-repository")
	revision, err := p.syncContent(ctx, checkout)
	if err != nil {
		return PublishResult{}, err
	}
	return Publish(PublishOptions{
		ContentDir: checkout, SiteRoot: filepath.Join(p.options.StateDir, "site"), BaseURL: p.options.BaseURL,
		SiteTitle: p.options.SiteTitle, Language: p.options.Language,
		ContentVersion: revision, EngineVersion: p.options.EngineVersion,
	})
}

func (p *publisher) syncContent(ctx context.Context, checkout string) (string, error) {
	if !isDirectory(filepath.Join(checkout, ".git")) {
		if _, err := os.Stat(checkout); err == nil {
			return "", fmt.Errorf("checkout path %s exists but is not a Git repository", checkout)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
			return "", err
		}
		if _, err := runGit(ctx, p.options.GitBinary, "", "clone", "--branch", p.options.ContentBranch, "--single-branch", p.options.ContentRepo, checkout); err != nil {
			return "", err
		}
	} else {
		if _, err := runGit(ctx, p.options.GitBinary, checkout, "remote", "set-url", "origin", p.options.ContentRepo); err != nil {
			return "", err
		}
		if _, err := runGit(ctx, p.options.GitBinary, checkout, "fetch", "--prune", "origin", p.options.ContentBranch); err != nil {
			return "", err
		}
		target := "origin/" + p.options.ContentBranch
		if _, err := runGit(ctx, p.options.GitBinary, checkout, "checkout", "--detach", target); err != nil {
			return "", err
		}
		if _, err := runGit(ctx, p.options.GitBinary, checkout, "reset", "--hard", target); err != nil {
			return "", err
		}
		if _, err := runGit(ctx, p.options.GitBinary, checkout, "clean", "-fd"); err != nil {
			return "", err
		}
	}
	return gitRevision(ctx, p.options.GitBinary, checkout)
}

func (p *publisher) health(response http.ResponseWriter, _ *http.Request) {
	if _, err := filepath.EvalSymlinks(filepath.Join(p.options.StateDir, "site", "current")); err != nil {
		http.Error(response, "site is not published", http.StatusServiceUnavailable)
		return
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte("ok\n"))
}

func (p *publisher) webhook(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, 1<<20))
	if err != nil {
		http.Error(response, "invalid payload", http.StatusBadRequest)
		return
	}
	if !validWebhookSignature(body, request.Header.Get("X-Hub-Signature-256"), p.options.WebhookSecret) {
		http.Error(response, "invalid signature", http.StatusUnauthorized)
		return
	}
	event := request.Header.Get("X-GitHub-Event")
	if event == "ping" {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("pong\n"))
		return
	}
	if event != "push" {
		response.WriteHeader(http.StatusAccepted)
		return
	}
	var payload struct {
		Ref        string `json:"ref"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(response, "invalid JSON", http.StatusBadRequest)
		return
	}
	if payload.Repository.FullName != p.options.ExpectedRepo {
		http.Error(response, "unexpected repository", http.StatusForbidden)
		return
	}
	if payload.Ref != "refs/heads/"+p.options.ContentBranch {
		response.WriteHeader(http.StatusAccepted)
		return
	}
	delivery := request.Header.Get("X-GitHub-Delivery")
	if delivery != "" && p.seenDelivery(delivery) {
		response.WriteHeader(http.StatusAccepted)
		return
	}
	p.enqueue()
	response.WriteHeader(http.StatusAccepted)
}

func (p *publisher) seenDelivery(delivery string) bool {
	p.deliveryMu.Lock()
	defer p.deliveryMu.Unlock()
	if _, exists := p.deliveries[delivery]; exists {
		return true
	}
	if len(p.deliveries) >= 1024 {
		p.deliveries = make(map[string]struct{})
	}
	p.deliveries[delivery] = struct{}{}
	return false
}

func validWebhookSignature(payload []byte, header, secret string) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	expected := mac.Sum(nil)
	return len(provided) == len(expected) && subtle.ConstantTimeCompare(provided, expected) == 1
}

func acquirePublishLock(name string) (*os.File, error) {
	file, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func releasePublishLock(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func PublisherContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
