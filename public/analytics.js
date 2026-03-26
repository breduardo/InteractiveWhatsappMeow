document.addEventListener("DOMContentLoaded", () => {
  const state = {
    sessions: [],
    volumeChart: null,
    sessionChart: null,
  };

  const sessionSelect = document.getElementById("analyticsSessionSelect");
  const rangeSelect = document.getElementById("analyticsRangeSelect");
  const metrics = document.getElementById("analyticsMetrics");
  const topChatsList = document.getElementById("topChatsList");
  const sessionBreakdownBody = document.getElementById("sessionBreakdownBody");

  async function loadSessions() {
    if (!App.hasApiKey()) {
      sessionSelect.innerHTML = '<option value="">Configure a API key</option>';
      return;
    }
    const payload = await App.apiFetch("/sessions");
    state.sessions = payload.sessions || [];
    App.renderSessionOptions(sessionSelect, state.sessions, { includeGlobal: true, selected: App.readQueryParam("sessionId") || "" });
  }

  function renderMetrics(totals) {
    const cards = [
      ["fa-message", totals.totalMessages || 0, "Mensagens no período"],
      ["fa-arrow-down", totals.inboundMessages || 0, "Inbound"],
      ["fa-arrow-up", totals.outboundMessages || 0, "Outbound"],
      ["fa-comments", totals.activeChats || 0, "Chats ativos"],
      ["fa-layer-group", totals.activeSessions || 0, "Sessões ativas"],
      ["fa-calendar", rangeSelect.value === "30d" ? 30 : 7, "Dias analisados"],
    ];

    metrics.innerHTML = cards
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
    topChatsList.innerHTML = App.missingApiKeyMarkup("As agregações de analytics dependem da X-API-Key.");
    sessionBreakdownBody.innerHTML = '<tr><td colspan="5">Defina a X-API-Key em Settings para carregar os dados.</td></tr>';
    if (state.volumeChart) {
      state.volumeChart.destroy();
      state.volumeChart = null;
    }
    if (state.sessionChart) {
      state.sessionChart.destroy();
      state.sessionChart = null;
    }
  }

  function renderTopChats(chats) {
    if (!chats.length) {
      topChatsList.innerHTML = '<div class="empty-state">Sem volume suficiente para montar ranking.</div>';
      return;
    }

    topChatsList.innerHTML = chats
      .map(
        (chat) => `
          <article class="session-card">
            <div class="session-card__top">
              <div>
                <h3 class="session-card__title">${App.escapeHTML(App.chatTitle(chat.chatJid))}</h3>
                <p class="session-card__meta">${App.escapeHTML(chat.sessionId)} · ${App.formatDateTime(chat.lastMessageAt)}</p>
              </div>
              <span class="subtle-pill">${chat.messageCount} msgs</span>
            </div>
            <p class="session-card__meta">${App.escapeHTML(chat.lastMessageText)}</p>
          </article>
        `
      )
      .join("");
  }

  function renderBreakdown(items) {
    if (!items.length) {
      sessionBreakdownBody.innerHTML = '<tr><td colspan="5">Nenhum dado encontrado.</td></tr>';
      return;
    }

    sessionBreakdownBody.innerHTML = items
      .map(
        (item) => `
          <tr>
            <td>${App.escapeHTML(item.name || item.sessionId)}</td>
            <td>${App.escapeHTML(App.statusLabel(item.status))}</td>
            <td>${item.totalMessages}</td>
            <td>${item.inboundMessages}</td>
            <td>${item.outboundMessages}</td>
          </tr>
        `
      )
      .join("");
  }

  function renderCharts(summary) {
    const labels = summary.dailySeries.map((item) => item.date.slice(5));
    const totals = summary.dailySeries.map((item) => item.totalMessages);
    const inbound = summary.dailySeries.map((item) => item.inboundMessages);
    const outbound = summary.dailySeries.map((item) => item.outboundMessages);

    if (state.volumeChart) {
      state.volumeChart.destroy();
    }
    state.volumeChart = new Chart(document.getElementById("volumeChart"), {
      type: "line",
      data: {
        labels,
        datasets: [
          {
            label: "Total",
            data: totals,
            borderColor: "#127f74",
            backgroundColor: "rgba(18, 127, 116, 0.16)",
            tension: 0.35,
            fill: true,
          },
          {
            label: "Inbound",
            data: inbound,
            borderColor: "#245d73",
            tension: 0.35,
          },
          {
            label: "Outbound",
            data: outbound,
            borderColor: "#d8941f",
            tension: 0.35,
          },
        ],
      },
      options: {
        maintainAspectRatio: false,
        plugins: {
          legend: { position: "bottom" },
        },
      },
    });

    if (state.sessionChart) {
      state.sessionChart.destroy();
    }
    state.sessionChart = new Chart(document.getElementById("sessionChart"), {
      type: "doughnut",
      data: {
        labels: summary.sessionBreakdown.map((item) => item.name || item.sessionId),
        datasets: [
          {
            data: summary.sessionBreakdown.map((item) => item.totalMessages),
            backgroundColor: ["#127f74", "#245d73", "#d8941f", "#c7524a", "#2db39d", "#729f95"],
          },
        ],
      },
      options: {
        maintainAspectRatio: false,
        plugins: {
          legend: { position: "bottom" },
        },
      },
    });
  }

  async function loadAnalytics() {
    if (!App.hasApiKey()) {
      renderLockedState();
      return;
    }
    const query = new URLSearchParams();
    if (sessionSelect.value) {
      query.set("sessionId", sessionSelect.value);
    }
    query.set("range", rangeSelect.value);

    const summary = await App.apiFetch(`/analytics/summary?${query.toString()}`);
    renderMetrics(summary.totals || {});
    renderTopChats(summary.topChats || []);
    renderBreakdown(summary.sessionBreakdown || []);
    renderCharts(summary);
  }

  document.getElementById("refreshAnalytics").addEventListener("click", () => {
    loadAnalytics().catch((error) => App.notify(error.message, "danger"));
  });
  sessionSelect.addEventListener("change", () => loadAnalytics().catch((error) => App.notify(error.message, "danger")));
  rangeSelect.addEventListener("change", () => loadAnalytics().catch((error) => App.notify(error.message, "danger")));

  Promise.resolve()
    .then(loadSessions)
    .then(loadAnalytics)
    .catch((error) => App.notify(error.message, "danger"));

  App.startPolling(loadAnalytics, App.getRefreshInterval("analytics", 30000));
});
