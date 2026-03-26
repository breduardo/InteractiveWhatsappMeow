(function () {
  const STORAGE_KEYS = {
    apiKey: "interactivewhatsmeow.apiKey",
    preferences: "interactivewhatsmeow.preferences",
  };

  const DEFAULT_PREFERENCES = {
    defaultSessionId: "",
    compactMode: false,
    refreshIntervals: {
      dashboard: 15000,
      chats: 10000,
      messages: 5000,
      analytics: 30000,
    },
  };

  function cloneDefaults() {
    return JSON.parse(JSON.stringify(DEFAULT_PREFERENCES));
  }

  const App = {
    apiBase: `${window.location.origin}/v1`,

    hasApiKey() {
      return this.getApiKey().length > 0;
    },

    getApiKey() {
      return (window.localStorage.getItem(STORAGE_KEYS.apiKey) || "").trim();
    },

    setApiKey(value) {
      window.localStorage.setItem(STORAGE_KEYS.apiKey, String(value || "").trim());
      this.refreshHeader();
    },

    getPreferences() {
      try {
        const raw = window.localStorage.getItem(STORAGE_KEYS.preferences);
        if (!raw) {
          return cloneDefaults();
        }
        const parsed = JSON.parse(raw);
        return {
          ...DEFAULT_PREFERENCES,
          ...parsed,
          refreshIntervals: {
            ...DEFAULT_PREFERENCES.refreshIntervals,
            ...(parsed.refreshIntervals || {}),
          },
        };
      } catch (error) {
        return cloneDefaults();
      }
    },

    savePreferences(patch) {
      const current = this.getPreferences();
      const next = {
        ...current,
        ...patch,
        refreshIntervals: {
          ...current.refreshIntervals,
          ...((patch && patch.refreshIntervals) || {}),
        },
      };
      window.localStorage.setItem(STORAGE_KEYS.preferences, JSON.stringify(next));
      this.refreshHeader();
      return next;
    },

    getDefaultSessionId() {
      return this.getPreferences().defaultSessionId || "";
    },

    setDefaultSessionId(value) {
      this.savePreferences({ defaultSessionId: String(value || "").trim() });
    },

    getRefreshInterval(key, fallback) {
      const preferences = this.getPreferences();
      const value = Number(preferences.refreshIntervals[key]);
      return Number.isFinite(value) && value > 0 ? value : fallback;
    },

    escapeHTML(value) {
      return String(value || "")
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#39;");
    },

    formatDateTime(value) {
      if (!value) {
        return "-";
      }
      return new Intl.DateTimeFormat("pt-BR", {
        dateStyle: "short",
        timeStyle: "short",
      }).format(new Date(value));
    },

    formatRelative(value) {
      if (!value) {
        return "-";
      }
      const target = new Date(value).getTime();
      const diffSeconds = Math.round((target - Date.now()) / 1000);
      const formatter = new Intl.RelativeTimeFormat("pt-BR", { numeric: "auto" });
      const ranges = [
        { limit: 60, unit: "second" },
        { limit: 3600, unit: "minute", size: 60 },
        { limit: 86400, unit: "hour", size: 3600 },
        { limit: 604800, unit: "day", size: 86400 },
      ];

      for (const range of ranges) {
        if (Math.abs(diffSeconds) < range.limit) {
          const amount = range.size ? Math.round(diffSeconds / range.size) : diffSeconds;
          return formatter.format(amount, range.unit);
        }
      }

      return this.formatDateTime(value);
    },

    statusTone(status) {
      switch (status) {
        case "connected":
          return "success";
        case "qr_ready":
        case "pairing":
        case "initializing":
          return "warning";
        case "error":
        case "logged_out":
        case "disconnected":
        case "deleted":
          return "danger";
        default:
          return "neutral";
      }
    },

    statusLabel(status) {
      const labels = {
        initializing: "Inicializando",
        qr_ready: "QR pronto",
        pairing: "Pareando",
        connected: "Conectada",
        disconnected: "Desconectada",
        logged_out: "Deslogada",
        deleted: "Removida",
        error: "Erro",
      };
      return labels[status] || status || "Indefinido";
    },

    directionLabel(direction) {
      return direction === "outbound" ? "Saída" : "Entrada";
    },

    previewText(message) {
      return (message && (message.text || message.mediaFileName || message.messageType)) || "Sem conteúdo";
    },

    initials(value) {
      const source = String(value || "").trim();
      if (!source) {
        return "WA";
      }
      return source.slice(0, 2).toUpperCase();
    },

    chatTitle(chatJid) {
      return String(chatJid || "")
        .replace("@s.whatsapp.net", "")
        .replace("@g.us", "");
    },

    toChatJID(value) {
      const trimmed = String(value || "").trim();
      if (!trimmed) {
        return "";
      }
      if (trimmed.includes("@")) {
        return trimmed;
      }
      return `${trimmed.replace(/\D+/g, "")}@s.whatsapp.net`;
    },

    missingApiKeyMarkup(message) {
      const copy = message || "Defina a X-API-Key em Settings para liberar as chamadas autenticadas da interface.";
      return `<div class="empty-state">${this.escapeHTML(copy)} <a href="/settings">Abrir Settings</a></div>`;
    },

    async apiFetch(path, options = {}) {
      const headers = new Headers(options.headers || {});
      const body = options.body;
      const apiKey = this.getApiKey();

      if (!apiKey) {
        const err = new Error("Defina a X-API-Key em Settings antes de usar a UI.");
        err.code = "missing_api_key";
        throw err;
      }

      if (!headers.has("Accept")) {
        headers.set("Accept", "application/json");
      }
      if (!(body instanceof FormData) && body !== undefined && body !== null && !headers.has("Content-Type")) {
        headers.set("Content-Type", "application/json");
      }
      if (apiKey) {
        headers.set("X-API-Key", apiKey);
      }

      const response = await fetch(`${this.apiBase}${path}`, {
        ...options,
        headers,
      });

      const raw = await response.text();
      let payload = null;
      if (raw) {
        try {
          payload = JSON.parse(raw);
        } catch (error) {
          payload = raw;
        }
      }

      if (!response.ok) {
        const message = payload && typeof payload === "object" && payload.error ? payload.error : `HTTP ${response.status}`;
        const err = new Error(message);
        err.status = response.status;
        err.payload = payload;
        throw err;
      }

      return payload;
    },

    notify(message, tone = "info") {
      let stack = document.querySelector(".toast-stack");
      if (!stack) {
        stack = document.createElement("div");
        stack.className = "toast-stack";
        document.body.appendChild(stack);
      }

      const toast = document.createElement("div");
      toast.className = "toast";
      toast.dataset.tone = tone;
      toast.textContent = message;
      stack.appendChild(toast);

      window.setTimeout(() => {
        toast.remove();
      }, 3600);
    },

    setLoading(element, isLoading) {
      if (!element) {
        return;
      }
      element.disabled = isLoading;
      if (isLoading) {
        element.dataset.originalLabel = element.textContent;
        element.textContent = "Carregando...";
      } else if (element.dataset.originalLabel) {
        element.textContent = element.dataset.originalLabel;
      }
    },

    startPolling(task, interval) {
      let stopped = false;

      const run = async () => {
        if (stopped || document.hidden) {
          return;
        }
        try {
          await task();
        } catch (error) {
          console.error(error);
        }
      };

      run();
      const timer = window.setInterval(run, interval);

      return () => {
        stopped = true;
        window.clearInterval(timer);
      };
    },

    renderSessionOptions(select, sessions, options = {}) {
      if (!select) {
        return;
      }

      const includeGlobal = Boolean(options.includeGlobal);
      const allowEmpty = Boolean(options.allowEmpty);
      const current = options.selected || select.value;

      const parts = [];
      if (includeGlobal) {
        parts.push('<option value="">Todas / Global</option>');
      } else if (allowEmpty) {
        parts.push('<option value="">Selecione</option>');
      }

      for (const session of sessions) {
        const selected = current === session.sessionId ? " selected" : "";
        parts.push(
          `<option value="${this.escapeHTML(session.sessionId)}"${selected}>${this.escapeHTML(session.name || session.sessionId)} (${this.escapeHTML(this.statusLabel(session.status))})</option>`
        );
      }

      select.innerHTML = parts.join("");
    },

    refreshHeader() {
      const status = document.querySelector("[data-global-status]");
      if (!status) {
        return;
      }

      const apiKey = this.getApiKey();
      const defaultSession = this.getDefaultSessionId();
      status.dataset.state = apiKey ? "active" : "inactive";

      const textNode = status.querySelector("[data-api-status-text]");
      const metaNode = status.querySelector("[data-api-status-meta]");
      if (textNode) {
        textNode.textContent = apiKey ? "API pronta para chamadas" : "API key ausente";
      }
      if (metaNode) {
        metaNode.textContent = defaultSession ? `Sessão padrão: ${defaultSession}` : "Defina a chave e a sessão padrão em Settings";
      }
    },

    readQueryParam(key) {
      return new URLSearchParams(window.location.search).get(key) || "";
    },
  };

  window.App = App;

  document.addEventListener("DOMContentLoaded", () => {
    const page = document.body.dataset.page;
    for (const link of document.querySelectorAll("[data-nav]")) {
      link.classList.toggle("is-active", link.dataset.nav === page);
    }
    App.refreshHeader();
  });
})();
