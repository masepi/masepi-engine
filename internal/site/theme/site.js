(() => {
  const root = document.documentElement;
  const button = document.querySelector('.theme-toggle');
  const stored = localStorage.getItem('theme');

  if (stored === 'light' || stored === 'dark') root.dataset.theme = stored;

  button?.addEventListener('click', () => {
    const dark = root.dataset.theme
      ? root.dataset.theme === 'dark'
      : matchMedia('(prefers-color-scheme: dark)').matches;
    root.dataset.theme = dark ? 'light' : 'dark';
    localStorage.setItem('theme', root.dataset.theme);
  });

  const prose = document.querySelector('.prose');
  if (prose && typeof window.renderMathInElement === 'function') {
    window.renderMathInElement(prose, {
      delimiters: [
        { left: '\\[', right: '\\]', display: true },
        { left: '\\(', right: '\\)', display: false },
      ],
      throwOnError: false,
      strict: 'warn',
      trust: false,
    });
  }
})();
