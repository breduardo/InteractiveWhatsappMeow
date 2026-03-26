document.addEventListener("DOMContentLoaded", () => {
  const state = {
    sessions: [],
    chats: [],
    sessionId: "",
    activeChatJid: "",
    messages: [],
    mode: "send",
    selectedMessage: null,
  };

  const sessionSelect = document.getElementById("chatSessionSelect");
  const chatSearch = document.getElementById("chatSearch");
  const chatList = document.getElementById("chatList");
  const messageList = document.getElementById("messageList");
  const threadTitle = document.getElementById("threadTitle");
  const threadMeta = document.getElementById("threadMeta");
  const composerForm = document.getElementById("composerForm");
  const composerInput = document.getElementById("composerInput");
  const composerFile = document.getElementById("composerFile");
  const composerMode = document.getElementById("composerMode");
  const quickChatForm = document.getElementById("quickChatForm");

  function setComposerMode(mode, message) {
    state.mode = mode;
    state.selectedMessage = message || null;
    if (mode === "send" || !message) {
      composerMode.classList.remove("is-visible");
      composerMode.textContent = "";
      return;
    }

    const label = mode === "edit" ? "Editando" : "Respondendo";
    composerMode.classList.add("is-visible");
    composerMode.textContent = `${label}: ${App.previewText(message)}`;
    if (mode === "edit") {
      composerInput.value = message.text || "";
    }
  }

  function renderChats() {
    const search = chatSearch.value.trim().toLowerCase();
    const filtered = state.chats.filter((chat) => {
      return !search || chat.chatJid.toLowerCase().includes(search) || (chat.lastMessageText || "").toLowerCase().includes(search);
    });

    if (!filtered.length) {
      chatList.innerHTML = '<div class="empty-state">Nenhuma conversa encontrada para esta sessão.</div>';
      return;
    }

    chatList.innerHTML = filtered
      .map(
        (chat) => `
          <article class="chat-item ${chat.chatJid === state.activeChatJid ? "is-active" : ""}" data-chat-jid="${App.escapeHTML(chat.chatJid)}">
            <div class="list-row">
              <h3 class="chat-item__title">${App.escapeHTML(App.chatTitle(chat.chatJid))}</h3>
              <span class="subtle-pill">${chat.messageCount}</span>
            </div>
            <p class="chat-item__excerpt">${App.escapeHTML(chat.lastMessageText || `[${chat.lastMessageType}]`)}</p>
            <p class="chat-item__meta">${App.directionLabel(chat.lastDirection)} · ${App.formatRelative(chat.lastMessageAt)}</p>
          </article>
        `
      )
      .join("");
  }

  function renderLockedState() {
    sessionSelect.innerHTML = '<option value="">Configure a API key</option>';
    chatList.innerHTML = App.missingApiKeyMarkup("Sem X-API-Key, a inbox não pode carregar sessões nem chats.");
    messageList.innerHTML = App.missingApiKeyMarkup("Defina a chave em Settings para abrir o histórico persistido.");
    threadTitle.textContent = "API key necessária";
    threadMeta.textContent = "A mensageria depende das rotas autenticadas em /v1.";
  }

  function renderMessages() {
    if (!state.activeChatJid) {
      messageList.innerHTML = '<div class="empty-state">Sem conversa selecionada.</div>';
      return;
    }

    if (!state.messages.length) {
      messageList.innerHTML = '<div class="empty-state">Ainda não existem mensagens persistidas para este chat.</div>';
      return;
    }

    const ordered = [...state.messages].sort((a, b) => new Date(a.messageTimestamp) - new Date(b.messageTimestamp));
    messageList.innerHTML = ordered
      .map((message) => {
        const isOutbound = message.direction === "outbound";
        return `
          <article class="message-bubble ${isOutbound ? "message-bubble--outbound" : ""}">
            <p class="message-bubble__text">${App.escapeHTML(App.previewText(message))}</p>
            <div class="message-bubble__meta">
              <span>${App.escapeHTML(message.messageType)}</span>
              <span>${App.escapeHTML(message.status)}</span>
              <span>${App.formatDateTime(message.messageTimestamp)}</span>
            </div>
            <div class="message-bubble__actions">
              <button type="button" data-action="reply" data-message-id="${App.escapeHTML(message.messageId)}">Responder</button>
              ${isOutbound ? `<button type="button" data-action="edit" data-message-id="${App.escapeHTML(message.messageId)}">Editar</button>` : ""}
            </div>
          </article>
        `;
      })
      .join("");
    messageList.scrollTop = messageList.scrollHeight;
  }

  async function loadSessions() {
    if (!App.hasApiKey()) {
      renderLockedState();
      return;
    }
    const payload = await App.apiFetch("/sessions");
    state.sessions = payload.sessions || [];
    const querySession = App.readQueryParam("sessionId");
    state.sessionId = querySession || App.getDefaultSessionId() || (state.sessions[0] && state.sessions[0].sessionId) || "";
    App.renderSessionOptions(sessionSelect, state.sessions, { allowEmpty: true, selected: state.sessionId });
    if (state.sessionId) {
      sessionSelect.value = state.sessionId;
      App.setDefaultSessionId(state.sessionId);
    }
  }

  async function loadChats() {
    if (!App.hasApiKey()) {
      renderLockedState();
      return;
    }
    if (!state.sessionId) {
      chatList.innerHTML = '<div class="empty-state">Defina uma sessão para listar os chats.</div>';
      return;
    }

    const payload = await App.apiFetch(`/chats?sessionId=${encodeURIComponent(state.sessionId)}`);
    state.chats = payload.chats || [];
    renderChats();

    const requestedChat = App.readQueryParam("chatJid");
    if (!state.activeChatJid) {
      state.activeChatJid = requestedChat || (state.chats[0] && state.chats[0].chatJid) || "";
    }
    if (state.activeChatJid) {
      await loadMessages();
      renderChats();
    }
  }

  async function loadMessages() {
    if (!App.hasApiKey()) {
      renderLockedState();
      return;
    }
    if (!state.sessionId || !state.activeChatJid) {
      renderMessages();
      return;
    }

    const payload = await App.apiFetch(
      `/messages?sessionId=${encodeURIComponent(state.sessionId)}&chatJid=${encodeURIComponent(state.activeChatJid)}&limit=60`
    );
    state.messages = payload.messages || [];
    threadTitle.textContent = App.chatTitle(state.activeChatJid);
    threadMeta.textContent = `${state.sessionId} · ${state.messages.length} mensagens carregadas`;
    renderMessages();
  }

  async function submitComposer(event) {
    event.preventDefault();
    if (!App.hasApiKey()) {
      App.notify("Defina a X-API-Key em Settings antes de enviar mensagens.", "warning");
      return;
    }
    if (!state.sessionId || !state.activeChatJid) {
      App.notify("Selecione uma sessão e um chat antes de enviar.", "warning");
      return;
    }

    const text = composerInput.value.trim();
    const file = composerFile.files[0];
    if (!file && !text) {
      App.notify("Digite uma mensagem ou selecione um arquivo.", "warning");
      return;
    }

    try {
      let result;
      if (file) {
        const formData = new FormData();
        formData.append("sessionId", state.sessionId);
        formData.append("to", state.activeChatJid);
        formData.append("caption", text);
        formData.append("file", file);
        result = await App.apiFetch("/messages/media", {
          method: "POST",
          body: formData,
        });
      } else if (state.mode === "reply" && state.selectedMessage) {
        result = await App.apiFetch("/messages/reply", {
          method: "POST",
          body: JSON.stringify({
            sessionId: state.sessionId,
            chatJid: state.activeChatJid,
            messageId: state.selectedMessage.messageId,
            text,
          }),
        });
      } else if (state.mode === "edit" && state.selectedMessage) {
        result = await App.apiFetch(`/messages/${encodeURIComponent(state.selectedMessage.messageId)}`, {
          method: "PUT",
          body: JSON.stringify({
            sessionId: state.sessionId,
            chatJid: state.activeChatJid,
            text,
          }),
        });
      } else {
        result = await App.apiFetch("/messages/text", {
          method: "POST",
          body: JSON.stringify({
            sessionId: state.sessionId,
            to: state.activeChatJid,
            text,
          }),
        });
      }

      App.notify(`Enviado para ${result && result.recipient ? result.recipient : state.activeChatJid} · ${result && result.messageId ? result.messageId : "sem id"}`, "success");
      composerForm.reset();
      setComposerMode("send", null);
      await loadChats();
      await loadMessages();
    } catch (error) {
      App.notify(error.message, "danger");
    }
  }

  async function openQuickChat(event) {
    event.preventDefault();
    if (!App.hasApiKey()) {
      App.notify("Defina a X-API-Key em Settings antes de abrir conversas manuais.", "warning");
      return;
    }
    if (!state.sessionId) {
      App.notify("Selecione uma sessão antes de abrir um destino manual.", "warning");
      return;
    }

    const target = App.toChatJID(document.getElementById("quickChatTarget").value);
    if (!target) {
      App.notify("Informe um telefone ou JID válido.", "warning");
      return;
    }
    state.activeChatJid = target;
    threadTitle.textContent = App.chatTitle(target);
    threadMeta.textContent = `${state.sessionId} · chat manual`;
    state.messages = [];
    renderChats();
    renderMessages();
  }

  chatList.addEventListener("click", async (event) => {
    const item = event.target.closest("[data-chat-jid]");
    if (!item) {
      return;
    }
    state.activeChatJid = item.dataset.chatJid;
    renderChats();
    await loadMessages();
  });

  messageList.addEventListener("click", (event) => {
    const button = event.target.closest("button[data-action]");
    if (!button) {
      return;
    }

    const message = state.messages.find((current) => current.messageId === button.dataset.messageId);
    if (!message) {
      return;
    }

    if (button.dataset.action === "reply") {
      setComposerMode("reply", message);
      composerInput.focus();
      return;
    }

    if (button.dataset.action === "edit") {
      setComposerMode("edit", message);
      composerInput.focus();
    }
  });

  sessionSelect.addEventListener("change", async () => {
    state.sessionId = sessionSelect.value;
    App.setDefaultSessionId(state.sessionId);
    state.activeChatJid = "";
    state.messages = [];
    await loadChats();
  });
  chatSearch.addEventListener("input", renderChats);
  composerForm.addEventListener("submit", submitComposer);
  quickChatForm.addEventListener("submit", openQuickChat);
  document.getElementById("cancelComposerMode").addEventListener("click", () => {
    composerForm.reset();
    setComposerMode("send", null);
  });
  document.getElementById("reloadMessages").addEventListener("click", () => {
    loadMessages().catch((error) => App.notify(error.message, "danger"));
  });

  Promise.resolve()
    .then(loadSessions)
    .then(loadChats)
    .catch((error) => App.notify(error.message, "danger"));

  App.startPolling(loadChats, App.getRefreshInterval("chats", 10000));
  App.startPolling(loadMessages, App.getRefreshInterval("messages", 5000));
});
