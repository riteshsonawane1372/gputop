document.querySelectorAll('[data-copy]').forEach((button) => {
  button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(button.dataset.copy);
      button.textContent = 'Copied!';
      setTimeout(() => { button.textContent = 'Copy'; }, 1600);
    } catch (_) { button.textContent = 'Select text'; }
  });
});
