document.addEventListener("DOMContentLoaded", () => {
  const state = {
    sessions: [],
  };

  const metricGrid = document.getElementById("metricGrid");
  const sessionList = document.getElementById("sessionList");
  const activityFeed = document.getElementById("activityFeed");
  const refreshButton = document.getElementById("refreshDashboard");
  const sessionForm = document.getElementById("sessionForm");
  const sessionSubmit = document.getElementById("sessionSubmit");
  const sessionModal = document.getElementById("sessionModal");
  const modalKicker = document.getElementById("modalKicker");
  const modalTitle = document.getElementById("modalTitle");
  const modalResult = document.getElementById("modalResult");
  const modalActions = document.getElementById("modalActions");
  const pairPhoneField = document.getElementById("pairPhoneField");
  const pairPhoneInput = document.getElementById("pairPhoneInput");

  function openModal(title, kicker) {
    modalTitle.textContent = title;
    modalKicker.textContent = kicker;
    modalResult.innerHTML = '<div class="empty-state">Carregando...</div>';
    modalActions.innerHTML = "";
    pairPhoneField.style.display = "none";
    sessionModal.classList.add("is-open");
    document.body.classList.add("modal-open");
  }

  function closeModal() {
    sessionModal.classList.remove("is-open");
    document.body.classList.remove("modal-open");
  }

  function renderMetrics(totals) {
    const cards = [
      ["fa-layer-group", totals.totalSessions || 0, "Sessões totais"],
      ["fa-wifi", totals.connectedSessions || 0, "Sessões conectadas"],
      ["fa-link", totals.activeWebhooks || 0, "Webhooks ativos"],
      ["fa-message", totals.messages24h || 0, "Mensagens nas últimas 24h"],
    ];

    metricGrid.innerHTML = cards
      .map(
        ([icon, value, label]) => `
          <article class="metric-card">
            <div class="metric-icon"><i class="fa-solid ${icon}"></i></div>
            <p class="metric-value">${App.escapeHTML(String(value))}</p>
            <p class="metric-label">${App.escapeHTML(label)}</p>
          </article>
        `
      )
      .join("");
  }

  function renderLockedState() {
    renderMetrics({});
    sessionList.innerHTML = App.missingApiKeyMarkup("Sem API key salva no navegador. O dashboard só consulta endpoints autenticados.");
    activityFeed.innerHTML = App.missingApiKeyMarkup("A timeline de atividade será carregada assim que a X-API-Key estiver configurada.");
  }

  function renderSessions(sessions) {
    if (!sessions.length) {
      sessionList.innerHTML = '<div class="empty-state">Nenhuma sessão criada até o momento.</div>';
      return;
    }

    sessionList.innerHTML = sessions
      .map((session) => {
        const phone = session.phone ? App.escapeHTML(session.phone) : "Sem telefone";
        const qrMeta = session.qrExpiresAt ? `QR expira ${App.formatRelative(session.qrExpiresAt)}` : "Sem QR ativo";
        return `
          <article class="session-card">
            <div class="session-card__top">
              <div>
                <h3 class="session-card__title">${App.escapeHTML(session.name || session.sessionId)}</h3>
                <p class="session-card__meta">${App.escapeHTML(session.sessionId)} · ${phone}</p>
              </div>
              <span class="status-pill" data-tone="${App.statusTone(session.status)}">${App.escapeHTML(App.statusLabel(session.status))}</span>
            </div>
            <div class="split-stat" style="margin-top: 1rem;">
              <div class="split-stat__item">
                <span class="section-kicker">Login</span>
                <span class="split-stat__value">${App.escapeHTML(session.loginMethod)}</span>
              </div>
              <div class="split-stat__item">
                <span class="section-kicker">QR / Pair</span>
                <span class="split-stat__value" style="font-size: 0.95rem;">${App.escapeHTML(qrMeta)}</span>
              </div>
            </div>
            <div class="session-actions" style="margin-top: 1rem;">
              <button class="button button--ghost" type="button" data-action="open-chat" data-session-id="${App.escapeHTML(session.sessionId)}"><i class="fa-solid fa-comments"></i> Chat</button>
              <button class="button button--soft" type="button" data-action="qr" data-session-id="${App.escapeHTML(session.sessionId)}"><i class="fa-solid fa-qrcode"></i> QR</button>
              <button class="button button--soft" type="button" data-action="pair" data-session-id="${App.escapeHTML(session.sessionId)}" data-phone="${App.escapeHTML(session.phone || "")}"><i class="fa-solid fa-mobile-screen"></i> Pair</button>
              <button class="button button--soft" type="button" data-action="reconnect" data-session-id="${App.escapeHTML(session.sessionId)}"><i class="fa-solid fa-plug-circle-bolt"></i> Reconnect</button>
              <button class="button button--danger" type="button" data-action="delete" data-session-id="${App.escapeHTML(session.sessionId)}"><i class="fa-solid fa-trash"></i> Remover</button>
            </div>
          </article>
        `;
      })
      .join("");
  }

  function renderActivity(items) {
    if (!items.length) {
      activityFeed.innerHTML = '<div class="empty-state">Ainda não há mensagens no histórico para alimentar a timeline.</div>';
      return;
    }

    activityFeed.innerHTML = items
      .map(
        (item) => `
          <article class="timeline-item">
            <div class="timeline-item__top">
              <h3 class="timeline-item__title">${App.escapeHTML(App.chatTitle(item.chatJid))}</h3>
              <span class="subtle-pill">${App.escapeHTML(App.directionLabel(item.direction))}</span>
            </div>
            <p class="timeline-item__meta">${App.escapeHTML(item.text || `[${item.messageType}]`)}</p>
            <p class="timeline-item__meta">${App.escapeHTML(item.sessionId)} · ${App.formatRelative(item.messageTimestamp)}</p>
          </article>
        `
      )
      .join("");
  }

  async function loadSummary() {
    if (!App.hasApiKey()) {
      renderLockedState();
      return;
    }
    const summary = await App.apiFetch("/dashboard/summary");
    state.sessions = summary.sessions || [];
    renderMetrics(summary.totals || {});
    renderSessions(state.sessions);
    renderActivity(summary.recentActivity || []);
  }

  async function handleCreateSession(event) {
    event.preventDefault();
    if (!App.hasApiKey()) {
      App.notify("Defina a X-API-Key em Settings antes de criar sessões.", "warning");
      return;
    }
    App.setLoading(sessionSubmit, true);

    const formData = new FormData(sessionForm);
    const payload = {
      sessionId: formData.get("sessionId"),
      name: formData.get("name"),
      loginMethod: formData.get("loginMethod"),
      phone: formData.get("phone"),
    };

    try {
      const result = await App.apiFetch("/sessions", {
        method: "POST",
        body: JSON.stringify(payload),
      });
      App.notify("Sessão criada.", "success");
      sessionForm.reset();
      await loadSummary();

      if (result && result.qr) {
        renderQRCodeModal(payload.sessionId, result.qr);
      } else if (result && result.pairCode) {
        renderPairCodeModal(payload.sessionId, result.pairCode);
      }
    } catch (error) {
      App.notify(error.message, "danger");
    } finally {
      App.setLoading(sessionSubmit, false);
    }
  }

  function renderQRCodeModal(sessionId, qr) {
    openModal(`QR code · ${sessionId}`, "Pareamento");
    modalResult.innerHTML = '<div class="qr-box"><div id="qrCanvas"></div></div>';
    if (window.QRCode) {
      new window.QRCode(document.getElementById("qrCanvas"), {
        text: qr.qrCode,
        width: 220,
        height: 220,
      });
    } else {
      modalResult.innerHTML = `<div class="code-box">${App.escapeHTML(qr.qrCode)}</div>`;
    }
    modalActions.innerHTML = `<span class="subtle-pill">Expira ${qr.expiresAt ? App.formatRelative(qr.expiresAt) : "sem prazo informado"}</span>`;
  }

  function renderPairCodeModal(sessionId, pairCode) {
    openModal(`Pairing code · ${sessionId}`, "Pareamento");
    modalResult.innerHTML = `<div class="code-box">${App.escapeHTML(pairCode.pairingCode)}</div>`;
    modalActions.innerHTML = `<span class="subtle-pill">${App.escapeHTML(pairCode.phone)}</span>`;
  }

  async function loadQRCode(sessionId) {
    openModal(`QR code · ${sessionId}`, "Pareamento");
    try {
      const qr = await App.apiFetch(`/sessions/${encodeURIComponent(sessionId)}/qr`);
      renderQRCodeModal(sessionId, qr);
    } catch (error) {
      modalResult.innerHTML = `<div class="empty-state">${App.escapeHTML(error.message)}</div>`;
    }
  }

  async function openPairCodeFlow(sessionId, phone) {
    openModal(`Pairing code · ${sessionId}`, "Pareamento");
    pairPhoneField.style.display = "grid";
    pairPhoneInput.value = phone || "";
    modalResult.innerHTML = '<div class="empty-state">Informe o telefone e gere um novo código.</div>';
    modalActions.innerHTML = '<button class="button" id="generatePairCodeButton" type="button"><i class="fa-solid fa-mobile-screen"></i> Gerar código</button>';
    document.getElementById("generatePairCodeButton").addEventListener("click", async () => {
      try {
        const result = await App.apiFetch(`/sessions/${encodeURIComponent(sessionId)}/pair-code`, {
          method: "POST",
          body: JSON.stringify({ phone: pairPhoneInput.value }),
        });
        renderPairCodeModal(sessionId, result);
      } catch (error) {
        modalResult.innerHTML = `<div class="empty-state">${App.escapeHTML(error.message)}</div>`;
      }
    });
  }

  async function reconnectSession(sessionId) {
    try {
      await App.apiFetch(`/sessions/${encodeURIComponent(sessionId)}/reconnect`, { method: "POST" });
      App.notify(`Reconnect solicitado para ${sessionId}.`, "success");
      await loadSummary();
    } catch (error) {
      App.notify(error.message, "danger");
    }
  }

  async function deleteSession(sessionId) {
    if (!window.confirm(`Remover a sessão ${sessionId}?`)) {
      return;
    }

    try {
      await App.apiFetch(`/sessions/${encodeURIComponent(sessionId)}`, { method: "DELETE" });
      App.notify(`Sessão ${sessionId} removida.`, "success");
      await loadSummary();
    } catch (error) {
      App.notify(error.message, "danger");
    }
  }

  sessionList.addEventListener("click", (event) => {
    const button = event.target.closest("button[data-action]");
    if (!button) {
      return;
    }

    const action = button.dataset.action;
    const sessionId = button.dataset.sessionId;
    const phone = button.dataset.phone || "";

    if (action === "open-chat") {
      App.setDefaultSessionId(sessionId);
      window.location.href = `/chat?sessionId=${encodeURIComponent(sessionId)}`;
      return;
    }
    if (action === "qr") {
      loadQRCode(sessionId);
      return;
    }
    if (action === "pair") {
      openPairCodeFlow(sessionId, phone);
      return;
    }
    if (action === "reconnect") {
      reconnectSession(sessionId);
      return;
    }
    if (action === "delete") {
      deleteSession(sessionId);
    }
  });

  document.getElementById("closeModal").addEventListener("click", closeModal);
  sessionModal.addEventListener("click", (event) => {
    if (event.target === sessionModal) {
      closeModal();
    }
  });
  refreshButton.addEventListener("click", () => loadSummary().catch((error) => App.notify(error.message, "danger")));
  sessionForm.addEventListener("submit", handleCreateSession);

  App.startPolling(loadSummary, App.getRefreshInterval("dashboard", 15000));
});
