document.addEventListener("DOMContentLoaded", () => {
  const state = {
    sessions: [],
  };

  const preferencesForm = document.getElementById("preferencesForm");
  const defaultSessionSelect = document.getElementById("defaultSessionSelect");
  const webhookSessionSelect = document.getElementById("webhookSessionSelect");
  const webhookForm = document.getElementById("webhookForm");
  const webhookList = document.getElementById("webhookList");
  const preferencesPreview = document.getElementById("preferencesPreview");

  function fillPreferences() {
    const preferences = App.getPreferences();
    document.getElementById("apiKeyInput").value = App.getApiKey();
    document.getElementById("dashboardInterval").value = preferences.refreshIntervals.dashboard;
    document.getElementById("chatInterval").value = preferences.refreshIntervals.chats;
    document.getElementById("messageInterval").value = preferences.refreshIntervals.messages;
    document.getElementById("analyticsInterval").value = preferences.refreshIntervals.analytics;
    defaultSessionSelect.value = preferences.defaultSessionId || "";

    preferencesPreview.innerHTML = `
      <div class="settings-list__item">API key: ${App.getApiKey() ? "configurada" : "ausente"}</div>
      <div class="settings-list__item">Sessão padrão: ${App.escapeHTML(preferences.defaultSessionId || "não definida")}</div>
      <div class="settings-list__item">Polling dashboard/chat/messages/analytics: ${preferences.refreshIntervals.dashboard}/${preferences.refreshIntervals.chats}/${preferences.refreshIntervals.messages}/${preferences.refreshIntervals.analytics} ms</div>
    `;
  }

  async function loadSessions() {
    if (!App.hasApiKey()) {
      defaultSessionSelect.innerHTML = '<option value="">Salve a API key primeiro</option>';
      webhookSessionSelect.innerHTML = '<option value="">Todas / Global</option>';
      return;
    }
    const payload = await App.apiFetch("/sessions");
    state.sessions = payload.sessions || [];
    App.renderSessionOptions(defaultSessionSelect, state.sessions, { allowEmpty: true, selected: App.getDefaultSessionId() });
    App.renderSessionOptions(webhookSessionSelect, state.sessions, { allowEmpty: false, includeGlobal: true });
  }

  async function loadWebhooks() {
    if (!App.hasApiKey()) {
      webhookList.innerHTML = App.missingApiKeyMarkup("Salve a X-API-Key localmente para listar e cadastrar webhooks.");
      return;
    }
    const payload = await App.apiFetch("/webhooks");
    const webhooks = payload.webhooks || [];

    if (!webhooks.length) {
      webhookList.innerHTML = '<div class="empty-state">Nenhum webhook cadastrado.</div>';
      return;
    }

    webhookList.innerHTML = webhooks
      .map(
        (webhook) => `
          <article class="webhook-item">
            <div class="webhook-item__top">
              <div>
                <h3 class="session-card__title">${App.escapeHTML(webhook.url)}</h3>
                <p class="session-card__meta">${App.escapeHTML(webhook.sessionId || "global")} · ${App.formatDateTime(webhook.createdAt)}</p>
              </div>
              <button class="button button--danger" type="button" data-delete-webhook="${webhook.id}"><i class="fa-solid fa-trash"></i> Remover</button>
            </div>
            <p class="session-card__meta">${(webhook.events || []).map((item) => App.escapeHTML(item)).join(" · ")}</p>
          </article>
        `
      )
      .join("");
  }

  preferencesForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    App.setApiKey(document.getElementById("apiKeyInput").value);
    App.savePreferences({
      defaultSessionId: defaultSessionSelect.value,
      refreshIntervals: {
        dashboard: Number(document.getElementById("dashboardInterval").value),
        chats: Number(document.getElementById("chatInterval").value),
        messages: Number(document.getElementById("messageInterval").value),
        analytics: Number(document.getElementById("analyticsInterval").value),
      },
    });
    fillPreferences();
    try {
      await loadSessions();
      await loadWebhooks();
      fillPreferences();
      App.notify("Preferências salvas localmente.", "success");
    } catch (error) {
      App.notify(error.message, "danger");
    }
  });

  webhookForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!App.hasApiKey()) {
      App.notify("Salve a X-API-Key antes de cadastrar webhooks.", "warning");
      return;
    }
    const url = document.getElementById("webhookUrl").value.trim();
    const events = Array.from(webhookForm.querySelectorAll('input[type="checkbox"]:checked')).map((input) => input.value);
    const sessionId = webhookSessionSelect.value.trim();

    try {
      await App.apiFetch("/webhooks", {
        method: "POST",
        body: JSON.stringify({
          sessionId: sessionId || null,
          url,
          events,
        }),
      });
      webhookForm.reset();
      App.renderSessionOptions(webhookSessionSelect, state.sessions, { allowEmpty: false, includeGlobal: true });
      await loadWebhooks();
      App.notify("Webhook criado.", "success");
    } catch (error) {
      App.notify(error.message, "danger");
    }
  });

  webhookList.addEventListener("click", async (event) => {
    const button = event.target.closest("[data-delete-webhook]");
    if (!button) {
      return;
    }

    try {
      await App.apiFetch(`/webhooks/${button.dataset.deleteWebhook}`, { method: "DELETE" });
      await loadWebhooks();
      App.notify("Webhook removido.", "success");
    } catch (error) {
      App.notify(error.message, "danger");
    }
  });

  document.getElementById("refreshWebhooks").addEventListener("click", () => {
    loadWebhooks().catch((error) => App.notify(error.message, "danger"));
  });

  fillPreferences();
  Promise.resolve()
    .then(loadSessions)
    .then(loadWebhooks)
    .catch((error) => App.notify(error.message, "danger"));
});
