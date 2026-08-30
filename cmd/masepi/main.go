package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/masepi/masepi-engine/internal/site"
)

const usage = `masepi — статический движок блога

Использование:
  masepi build [флаги]   собрать сайт
  masepi serve [флаги]   запустить локальный сервер для уже собранного сайта
  masepi publisher       запустить сервер публикации и GitHub webhook
  masepi publish-once    синхронизировать контент и опубликовать один релиз
  masepi version         вывести версию

Команда build:
  -content PATH          каталог контента (по умолчанию content)
  -output PATH           каталог результата (по умолчанию dist)
  -base-url URL          публичный URL сайта
  -site-title TEXT       название сайта
  -language TAG          язык HTML в формате BCP 47

Команда serve:
  -dir PATH              каталог результата (по умолчанию dist)
  -addr ADDRESS          адрес сервера (по умолчанию 127.0.0.1:8080)
`

var version = "dev"

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "build":
		build(os.Args[2:])
	case "serve":
		serve(os.Args[2:])
	case "publisher":
		publisher(os.Args[2:], false)
	case "publish-once":
		publisher(os.Args[2:], true)
	case "version", "--version", "-version":
		fmt.Println("masepi", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		log.Printf("неизвестная команда %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func publisher(arguments []string, once bool) {
	flags := flag.NewFlagSet("publisher", flag.ExitOnError)
	repository := flags.String("repo", env("CONTENT_REPO", ""), "Git-репозиторий контента")
	branch := flags.String("branch", env("CONTENT_BRANCH", ""), "ветка контента")
	expectedRepository := flags.String("github-repository", env("WEBHOOK_REPOSITORY", ""), "GitHub owner/repository")
	baseURL := flags.String("base-url", env("BASE_URL", ""), "публичный URL")
	siteTitle := flags.String("site-title", env("SITE_TITLE", ""), "название сайта")
	language := flags.String("language", env("SITE_LANGUAGE", ""), "язык HTML")
	secret := flags.String("webhook-secret", env("WEBHOOK_SECRET", ""), "секрет GitHub webhook")
	address := flags.String("addr", env("PUBLISHER_ADDR", "0.0.0.0:8080"), "адрес webhook-сервера")
	poll := flags.Duration("poll-interval", envDuration("POLL_INTERVAL", 5*time.Minute), "интервал резервной проверки")
	flags.Parse(arguments)
	options := site.PublisherOptions{
		ContentRepo: *repository, ContentBranch: *branch,
		ExpectedRepo: *expectedRepository, StateDir: "/data", BaseURL: *baseURL,
		SiteTitle: *siteTitle, Language: *language,
		WebhookSecret: *secret, ListenAddr: *address, PollInterval: *poll,
		EngineVersion: version,
	}
	ctx, stop := site.PublisherContext()
	defer stop()
	if once {
		result, err := site.PublishOnce(ctx, options)
		if err != nil {
			log.Fatal("ошибка публикации: ", err)
		}
		if result.Release == "" {
			log.Printf("уже опубликовано: %s", result.Revision)
		} else {
			log.Printf("опубликован релиз %s", result.Release)
		}
		return
	}
	if err := site.RunPublisher(ctx, options); err != nil {
		log.Fatal("ошибка publisher: ", err)
	}
}

func env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := env(name, "")
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		log.Fatalf("некорректный %s: %v", name, err)
	}
	return parsed
}

func build(arguments []string) {
	flags := flag.NewFlagSet("build", flag.ExitOnError)
	content := flags.String("content", "content", "каталог контента")
	output := flags.String("output", "dist", "каталог результата")
	baseURL := flags.String("base-url", "", "публичный URL")
	siteTitle := flags.String("site-title", env("SITE_TITLE", ""), "название сайта")
	language := flags.String("language", env("SITE_LANGUAGE", ""), "язык HTML")
	flags.Parse(arguments)

	result, err := site.Build(site.Options{
		ContentDir: *content, OutputDir: *output, BaseURL: *baseURL, SiteTitle: *siteTitle, Language: *language,
	})
	if err != nil {
		log.Fatal("ошибка сборки: ", err)
	}
	log.Printf("готово: %d пост(ов), %d файл(ов) ассетов → %s", result.Posts, result.Assets, result.Output)
}

func serve(arguments []string) {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	directory := flags.String("dir", "dist", "каталог результата")
	address := flags.String("addr", "127.0.0.1:8080", "адрес сервера")
	flags.Parse(arguments)
	if err := site.Serve(*directory, *address); err != nil {
		log.Fatal("ошибка сервера: ", err)
	}
}
