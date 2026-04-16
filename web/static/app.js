// debeasy — small UI glue: tab switching, Ctrl+Enter to run query, modal helpers
(function () {
  function activateTab(name) {
    document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.tab === name));
    document.querySelectorAll('.tab-pane').forEach(p => p.classList.toggle('active', p.dataset.pane === name));
  }
  window.__activateTab = activateTab;

  document.addEventListener('click', function (e) {
    const t = e.target.closest('.tab');
    if (!t) return;
    activateTab(t.dataset.tab);
  });

  // Ctrl/Cmd + Enter inside the SQL editor submits the form
  document.addEventListener('keydown', function (e) {
    const editor = document.getElementById('sql');
    if (!editor || document.activeElement !== editor) return;
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault();
      const form = document.getElementById('query-form');
      if (form) htmx.trigger(form, 'submit');
    }
  });

  // ESC closes modal
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') {
      const m = document.getElementById('modal');
      if (m && m.innerHTML.trim() !== '') { m.innerHTML = ''; }
    }
  });

  // After successful row save / DDL change → tree-refresh trigger refreshes sidebar tree
  // (htmx handles this via the trigger header on the response)
})();
