(function () {
  'use strict';

  var state = {
    file: null,
    languages: [],
    jobId: null,
    eventSource: null,
    lastPath: '',
  };

  var el = function (id) { return document.getElementById(id); };

  // ---- networking ------------------------------------------------------

  function postJSON(url, body) {
    return fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {}),
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) throw new Error(friendlyError(data));
        return data;
      });
    });
  }

  function fetchJSON(url) {
    return fetch(url).then(function (res) {
      if (!res.ok) throw new Error('request failed');
      return res.json();
    });
  }

  // Every error this app's Go side produces is already a short, specific,
  // presentable sentence (see internal/engine's classifyOCRError) — this
  // just guards against a missing/empty message rather than translating
  // known codes, unlike document-converter's typst-specific override.
  function friendlyError(data) {
    if (data && data.error) return data.error;
    return 'Something went wrong.';
  }

  // ---- app-mode window auto-resize ------------------------------------

  // Chrome's --app=<url> mode opens a real, single, chrome-less window
  // (no tabs, no address bar) rather than a tab in the user's normal
  // browser — and unlike a normal tab, script-driven resizeTo() is
  // actually honored on that kind of window. So instead of a fixed size,
  // the window tracks the content's real height: small for just the drop
  // zone, taller once the workspace/progress/done sections appear —
  // rather than reserving space for a state that isn't showing.
  function setupAutoResize() {
    if (typeof window.resizeTo !== 'function' || typeof ResizeObserver === 'undefined') return;

    var targetInnerWidth = 640;
    var lastHeight = 0;

    function fit() {
      var contentHeight = Math.ceil(document.documentElement.getBoundingClientRect().height);
      if (Math.abs(contentHeight - lastHeight) < 2) return;
      lastHeight = contentHeight;

      var chromeH = Math.max(0, window.outerHeight - window.innerHeight);
      var chromeW = Math.max(0, window.outerWidth - window.innerWidth);
      var maxH = (window.screen.availHeight || 1000) - 60;

      var h = Math.min(maxH, contentHeight + chromeH);
      var w = targetInnerWidth + chromeW;
      try { window.resizeTo(w, h); } catch (e) { /* not resizable in this context; fine */ }
    }

    new ResizeObserver(fit).observe(document.documentElement);
    fit();
  }

  // ---- boot --------------------------------------------------------

  function boot() {
    bindEvents();
    setupAutoResize();
    fetchJSON('/api/languages').then(function (data) {
      state.languages = data.languages || [];
      populateLanguageSelect();
    }).catch(function () {
      showError("Couldn't load the list of OCR languages. Is the server running?");
    });
  }

  function bindEvents() {
    dropZone.addEventListener('click', function () { fileInput.click(); });
    fileInput.addEventListener('change', function () {
      if (fileInput.files && fileInput.files[0]) loadFile(fileInput.files[0]);
    });
    ['dragenter', 'dragover'].forEach(function (evt) {
      dropZone.addEventListener(evt, function (e) { e.preventDefault(); dropZone.classList.add('dragover'); });
    });
    ['dragleave', 'drop'].forEach(function (evt) {
      dropZone.addEventListener(evt, function (e) { e.preventDefault(); dropZone.classList.remove('dragover'); });
    });
    dropZone.addEventListener('drop', function (e) {
      if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0]) loadFile(e.dataTransfer.files[0]);
    });

    el('runBtn').addEventListener('click', onRun);
    el('cancelJob').addEventListener('click', onCancelJob);
    el('openFile').addEventListener('click', function () { if (state.lastPath) postJSON('/api/open', { path: state.lastPath }); });
    el('showFolder').addEventListener('click', function () { if (state.lastPath) postJSON('/api/reveal', { path: state.lastPath }); });

    el('startOver').addEventListener('click', function () {
      closeJob();
      state.file = null;
      el('workspace').classList.add('hidden');
      el('doneActions').classList.add('hidden');
      hideError();
      dropZone.classList.remove('hidden');
      fileInput.value = '';
    });
  }

  var dropZone = el('dropZone');
  var fileInput = el('fileInput');

  // ---- languages ------------------------------------------------

  function populateLanguageSelect() {
    var sel = el('language');
    sel.innerHTML = '';
    state.languages.forEach(function (l) {
      var opt = document.createElement('option');
      opt.value = l.code;
      opt.textContent = l.label;
      sel.appendChild(opt);
    });
    var hasEnglish = state.languages.some(function (l) { return l.code === 'eng'; });
    sel.value = hasEnglish ? 'eng' : (state.languages[0] ? state.languages[0].code : '');
  }

  // ---- file selection ------------------------------------------------

  function loadFile(file) {
    hideError();
    hideDone();

    if (!/\.pdf$/i.test(file.name)) {
      showError('Only PDF files are supported.');
      return;
    }

    state.file = file;
    el('fileName').textContent = file.name;

    dropZone.classList.add('hidden');
    el('workspace').classList.remove('hidden');
  }

  // ---- run / jobs -------------------------------------------------

  function onRun() {
    if (!state.file) return;
    hideError();
    hideDone();
    setRunning(true);
    showProgress();

    var form = new FormData();
    form.append('file', state.file, state.file.name);
    form.append('language', el('language').value);
    form.append('ocrExistingText', el('ocrExistingText').checked ? 'true' : 'false');
    form.append('cleanup', el('cleanup').checked ? 'true' : 'false');

    fetch('/api/jobs', { method: 'POST', body: form })
      .then(function (res) {
        return res.json().catch(function () { return {}; }).then(function (data) {
          if (!res.ok) throw new Error(friendlyError(data));
          return data;
        });
      })
      .then(function (data) {
        state.jobId = data.jobId;
        subscribeJob(data.jobId);
      })
      .catch(function (err) {
        setRunning(false);
        hideProgress();
        showError(String(err.message || err));
      });
  }

  // If the SSE connection drops before a terminal event arrives (server
  // restarted, network hiccup, EventSource stuck retrying against a dead
  // job) don't leave the UI frozen on "Running OCR…" forever. ocrmypdf can
  // legitimately run for minutes on a large scanned document with no
  // intermediate output to heartbeat on, so this only needs to outlast a
  // real stall, not a normal slow run.
  var STALL_TIMEOUT_MS = 20000;

  function subscribeJob(jobId) {
    var es = new EventSource('/api/jobs/' + jobId + '/events');
    state.eventSource = es;

    var stallTimer = null;
    function resetStallTimer() {
      clearTimeout(stallTimer);
      stallTimer = setTimeout(function () {
        closeJob();
        setRunning(false);
        hideProgress();
        showError('Lost connection to PDF OCR. Is it still running?');
      }, STALL_TIMEOUT_MS);
    }
    resetStallTimer();

    es.onmessage = function (msg) {
      resetStallTimer();
      var e = JSON.parse(msg.data);
      if (e.stage === 'done') {
        clearTimeout(stallTimer);
        closeJob();
        setRunning(false);
        hideProgress();
        showDone(e);
      } else if (e.stage === 'error') {
        clearTimeout(stallTimer);
        closeJob();
        setRunning(false);
        hideProgress();
        showError(friendlyError({ error: e.message }));
      } else if (e.stage === 'canceled') {
        clearTimeout(stallTimer);
        closeJob();
        setRunning(false);
        hideProgress();
      }
    };
    es.onerror = function () {
      clearTimeout(stallTimer);
      closeJob();
      setRunning(false);
      hideProgress();
      showError('Lost connection to the local server.');
    };
  }

  function onCancelJob() {
    if (!state.jobId) return;
    postJSON('/api/jobs/' + state.jobId + '/cancel', {}).catch(function () {});
  }

  function closeJob() {
    if (state.eventSource) { state.eventSource.close(); state.eventSource = null; }
    state.jobId = null;
  }

  function showDone(e) {
    el('doneActions').classList.remove('hidden');
    el('doneMessage').textContent = (e.filename || 'Saved.') + ' — saved to Downloads.';
    state.lastPath = e.path || '';
  }

  function hideDone() {
    el('doneActions').classList.add('hidden');
  }

  // ---- small UI state helpers ----------------------------------------

  function setRunning(v) { el('runBtn').disabled = v; }
  function showError(msg) { el('error').textContent = msg; el('error').classList.remove('hidden'); }
  function hideError() { el('error').classList.add('hidden'); }
  function showProgress() { el('progress').classList.remove('hidden'); el('progressLabel').textContent = 'Running OCR…'; }
  function hideProgress() { el('progress').classList.add('hidden'); }

  boot();
})();
