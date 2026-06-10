import './styles.scss';

const maxFileSize = 10 * 1024 * 1024;
const supportedTypes = new Set(['image/jpeg', 'image/png', 'image/webp']);

document.querySelector('#app').innerHTML = `
  <div class="page">
    <header class="header">
      <div>
        <p class="eyebrow">GophProfile</p>
        <h1 class="title">Управление аватарками</h1>
        <p class="subtitle">Загрузите изображение, проверьте ответ API и посмотрите галерею пользователя</p>
      </div>
      <div class="api-status" id="apiStatus">API не проверен</div>
    </header>

    <main class="workspace">
      <section class="panel upload-panel">
        <div class="panel__header">
          <h2 class="panel__title">Загрузка</h2>
        </div>

        <form class="panel__body form" id="uploadForm">
          <label class="field">
            <span class="field__label">Email</span>
            <input class="input" id="userEmail" name="email" type="email" autocomplete="email" required
              placeholder="user@example.com">
            <span class="field__hint">Email будет связан с внутренним UUID пользователя</span>
          </label>

          <label class="field">
            <span class="field__label">User ID</span>
            <input class="input" id="uploadUserID" name="user_id" type="text" autocomplete="off" readonly
              placeholder="6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e">
            <span class="field__hint">Внутренний UUID для header X-User-ID создается или находится по email</span>
          </label>

          <div class="actions">
            <button class="button button--secondary" id="resolveUserButton" type="button">Получить UUID</button>
          </div>

          <label class="field">
            <span class="field__label">Файл</span>
            <input class="input input--file" id="avatarFile" name="file" type="file"
              accept="image/jpeg,image/png,image/webp" required>
            <span class="field__hint">JPEG, PNG или WebP, до 10MB</span>
          </label>

          <div class="preview" id="preview">
            <img class="preview__image" id="previewImage" alt="Предпросмотр аватарки">
            <div class="preview__content">
              <p class="preview__name" id="previewName"></p>
              <p class="preview__meta" id="previewMeta"></p>
            </div>
          </div>

          <div class="actions">
            <button class="button button--primary" id="uploadButton" type="submit">Загрузить</button>
            <button class="button button--secondary" id="resetButton" type="reset">Сбросить</button>
          </div>

          <div class="notice" id="uploadNotice"></div>
        </form>
      </section>

      <section class="panel">
        <div class="panel__header">
          <h2 class="panel__title">Галерея и API</h2>
        </div>

        <div class="panel__body">
          <div class="tabs" role="tablist" aria-label="Разделы интерфейса">
            <button class="tab is-active" type="button" data-tab="gallery">Галерея</button>
            <button class="tab" type="button" data-tab="response">Ответ API</button>
          </div>

          <section class="tab-panel is-active" id="galleryPanel">
            <div class="toolbar">
              <input class="input" id="galleryEmail" type="email" autocomplete="email"
                placeholder="Email для публичного поиска">
              <button class="button button--secondary" id="resolveGalleryButton" type="button">Найти</button>
            </div>
            <div class="toolbar">
              <input class="input" id="galleryUserID" type="text" autocomplete="off"
                placeholder="User ID для галереи">
              <button class="button button--secondary" id="loadGalleryButton" type="button">Обновить</button>
            </div>
            <div class="avatar-list" id="avatarList">
              <div class="empty-state">Загрузите аватарку или укажите user_id</div>
            </div>
          </section>

          <section class="tab-panel" id="responsePanel">
            <pre class="json-output" id="responseOutput">Ответы API появятся здесь</pre>
          </section>
        </div>
      </section>
    </main>
  </div>
`;

const uploadForm = document.querySelector('#uploadForm');
const userEmail = document.querySelector('#userEmail');
const uploadUserID = document.querySelector('#uploadUserID');
const avatarFile = document.querySelector('#avatarFile');
const uploadButton = document.querySelector('#uploadButton');
const resolveUserButton = document.querySelector('#resolveUserButton');
const resetButton = document.querySelector('#resetButton');
const uploadNotice = document.querySelector('#uploadNotice');
const preview = document.querySelector('#preview');
const previewImage = document.querySelector('#previewImage');
const previewName = document.querySelector('#previewName');
const previewMeta = document.querySelector('#previewMeta');
const galleryEmail = document.querySelector('#galleryEmail');
const galleryUserID = document.querySelector('#galleryUserID');
const loadGalleryButton = document.querySelector('#loadGalleryButton');
const resolveGalleryButton = document.querySelector('#resolveGalleryButton');
const avatarList = document.querySelector('#avatarList');
const responseOutput = document.querySelector('#responseOutput');
const apiStatus = document.querySelector('#apiStatus');
const tabs = document.querySelectorAll('.tab');

document.addEventListener('DOMContentLoaded', checkHealth);

tabs.forEach((tab) => {
  tab.addEventListener('click', () => activateTab(tab.dataset.tab));
});

avatarFile.addEventListener('change', () => {
  const file = avatarFile.files[0];
  hideNotice();

  if (!file) {
    clearPreview();
    return;
  }

  const validationError = validateFile(file);
  if (validationError) {
    showNotice(validationError, 'error');
    avatarFile.value = '';
    clearPreview();
    return;
  }

  previewImage.src = URL.createObjectURL(file);
  previewName.textContent = file.name;
  previewMeta.textContent = `${file.type}, ${formatBytes(file.size)}`;
  preview.classList.add('is-visible');
});

resetButton.addEventListener('click', () => {
  clearPreview();
  hideNotice();
});

resolveUserButton.addEventListener('click', async () => {
  await resolveUploadUser();
});

uploadForm.addEventListener('submit', async (event) => {
  event.preventDefault();
  hideNotice();

  let userID = normalizeUserID(uploadUserID.value);
  const email = normalizeEmail(userEmail.value);
  const file = avatarFile.files[0];

  const emailError = validateEmail(email);
  if (emailError) {
    showNotice(emailError, 'error');
    return;
  }

  if (!userID) {
    userID = await resolveUploadUser();
    if (!userID) {
      return;
    }
  }

  const userIDError = validateUserID(userID);
  if (userIDError) {
    showNotice(userIDError, 'error');
    return;
  }

  if (!file) {
    showNotice('Выберите файл изображения', 'error');
    return;
  }

  const validationError = validateFile(file);
  if (validationError) {
    showNotice(validationError, 'error');
    return;
  }

  const formData = new FormData();
  formData.append('file', file);
  setUploadLoading(true);

  try {
    const response = await fetch('/api/v1/avatars', {
      method: 'POST',
      headers: {
        'X-User-ID': userID
      },
      body: formData
    });

    const data = await readResponse(response);
    renderApiResponse(response, data);

    if (!response.ok) {
      showNotice(`Ошибка загрузки: HTTP ${response.status}`, 'error');
      activateTab('response');
      return;
    }

    showNotice('Аватарка отправлена на обработку', 'success');
    galleryUserID.value = data?.user_id || userID;
    galleryEmail.value = email;
    activateTab('gallery');
    await loadGallery(data?.user_id || userID);
  } catch (error) {
    showNotice(`Ошибка сети: ${error.message}`, 'error');
  } finally {
    setUploadLoading(false);
  }
});

loadGalleryButton.addEventListener('click', async () => {
  const userID = normalizeUserID(galleryUserID.value);
  const userIDError = validateUserID(userID);

  if (userIDError) {
    renderEmptyState(userIDError);
    return;
  }

  galleryUserID.value = userID;
  await loadGallery(userID);
});

resolveGalleryButton.addEventListener('click', async () => {
  const email = normalizeEmail(galleryEmail.value);
  const emailError = validateEmail(email);

  if (emailError) {
    renderEmptyState(emailError);
    return;
  }

  const user = await resolveUserByEmail(email);
  if (!user) {
    return;
  }

  galleryEmail.value = user.email;
  galleryUserID.value = user.user_id || user.id;
  await loadGallery(user.user_id || user.id);
});

async function checkHealth() {
  try {
    const response = await fetch('/health');
    apiStatus.textContent = response.ok ? 'API доступен' : `API HTTP ${response.status}`;
    apiStatus.classList.toggle('is-ok', response.ok);
  } catch {
    apiStatus.textContent = 'API недоступен';
    apiStatus.classList.remove('is-ok');
  }
}

async function resolveUploadUser() {
  const email = normalizeEmail(userEmail.value);
  const emailError = validateEmail(email);

  if (emailError) {
    showNotice(emailError, 'error');
    return '';
  }

  setResolveLoading(true);

  try {
    const user = await resolveUserByEmail(email);
    if (!user) {
      showNotice('Не удалось получить UUID пользователя', 'error');
      return '';
    }

    const userID = user.user_id || user.id;
    userEmail.value = user.email;
    uploadUserID.value = userID;
    galleryEmail.value = user.email;
    galleryUserID.value = userID;
    showNotice(`UUID пользователя: ${userID}`, 'success');
    return userID;
  } catch (error) {
    showNotice(`Ошибка сети: ${error.message}`, 'error');
    return '';
  } finally {
    setResolveLoading(false);
  }
}

async function resolveUserByEmail(email) {
  const response = await fetch('/api/v1/users/resolve', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ email })
  });
  const data = await readResponse(response);
  renderApiResponse(response, data);

  if (!response.ok) {
    activateTab('response');
    return null;
  }

  return data;
}

async function loadGallery(userID) {
  loadGalleryButton.disabled = true;
  renderEmptyState('Загрузка галереи');

  try {
    const response = await fetch(`/api/v1/users/${encodeURIComponent(userID)}/avatars`);
    const data = await readResponse(response);
    renderApiResponse(response, data);

    if (!response.ok) {
      renderEmptyState(`Не удалось загрузить галерею: HTTP ${response.status}`);
      return;
    }

    renderGallery(normalizeAvatarList(data));
  } catch (error) {
    renderEmptyState(`Ошибка сети: ${error.message}`);
  } finally {
    loadGalleryButton.disabled = false;
  }
}

function renderGallery(avatars) {
  avatarList.innerHTML = '';

  if (!avatars.length) {
    renderEmptyState('У пользователя пока нет аватарок');
    return;
  }

  avatars.forEach((avatar) => {
    const id = avatar.id || avatar.avatar_id;
    const userID = avatar.user_id || galleryUserID.value.trim();
    const imageUrl = avatar.url || `/api/v1/avatars/${encodeURIComponent(id)}?size=100x100`;
    const card = document.createElement('article');
    const image = document.createElement('img');
    const content = document.createElement('div');
    const title = document.createElement('p');
    const meta = document.createElement('p');
    const actions = document.createElement('div');
    const metadataButton = document.createElement('button');
    const openButton = document.createElement('a');
    const deleteButton = document.createElement('button');

    card.className = 'avatar-card';
    image.className = 'avatar-card__image';
    content.className = 'avatar-card__content';
    title.className = 'avatar-card__title';
    meta.className = 'avatar-card__meta';
    actions.className = 'actions';
    metadataButton.className = 'button button--secondary';
    openButton.className = 'button button--secondary';
    deleteButton.className = 'button button--danger';

    image.src = imageUrl;
    image.alt = `Аватарка ${id || ''}`;
    title.textContent = id || 'Без ID';
    meta.textContent = [
      userID ? `user_id: ${userID}` : null,
      avatar.status ? `status: ${avatar.status}` : null,
      avatar.processing_status ? `processing: ${avatar.processing_status}` : null
    ].filter(Boolean).join(', ');

    metadataButton.type = 'button';
    metadataButton.textContent = 'Метаданные';
    metadataButton.disabled = !id;
    metadataButton.addEventListener('click', () => loadMetadata(id));

    openButton.href = id ? `/api/v1/avatars/${encodeURIComponent(id)}` : '#';
    openButton.target = '_blank';
    openButton.rel = 'noreferrer';
    openButton.textContent = 'Открыть';

    deleteButton.type = 'button';
    deleteButton.textContent = 'Удалить';
    deleteButton.disabled = !id || !userID;
    deleteButton.addEventListener('click', () => deleteAvatar(id, userID));

    actions.append(metadataButton, openButton, deleteButton);
    content.append(title, meta, actions);
    card.append(image, content);
    avatarList.append(card);
  });
}

async function loadMetadata(avatarId) {
  try {
    const response = await fetch(`/api/v1/avatars/${encodeURIComponent(avatarId)}/metadata`);
    const data = await readResponse(response);
    renderApiResponse(response, data);
  } catch (error) {
    setResponseText(`Ошибка сети: ${error.message}`);
  }

  activateTab('response');
}

async function deleteAvatar(avatarId, userID) {
  try {
    const response = await fetch(`/api/v1/avatars/${encodeURIComponent(avatarId)}`, {
      method: 'DELETE',
      headers: {
        'X-User-ID': userID
      }
    });
    const data = await readResponse(response);

    renderApiResponse(response, data || { status: 'deleted' });

    if (response.ok) {
      await loadGallery(userID);
    } else {
      activateTab('response');
    }
  } catch (error) {
    setResponseText(`Ошибка сети: ${error.message}`);
    activateTab('response');
  }
}

function normalizeUserID(value) {
  return value.trim().toLowerCase();
}

function normalizeEmail(value) {
  return value.trim().toLowerCase();
}

function validateEmail(value) {
  if (!value) {
    return 'Укажите email пользователя';
  }

  if (value.length > 254 || /\s/.test(value) || !/^[^@]+@[^@]+\.[^@]+$/.test(value)) {
    return 'Укажите корректный email пользователя';
  }

  return '';
}

function validateUserID(value) {
  if (!value) {
    return 'Укажите user_id пользователя';
  }

  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)) {
    return 'User ID должен быть UUID из ответа API';
  }

  return '';
}

function validateFile(file) {
  if (!supportedTypes.has(file.type)) {
    return 'Поддерживаются только JPEG, PNG и WebP';
  }

  if (file.size > maxFileSize) {
    return 'Размер файла не должен превышать 10MB';
  }

  return '';
}

async function readResponse(response) {
  if (response.status === 204) {
    return null;
  }

  const text = await response.text();

  if (!text) {
    return null;
  }

  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function renderApiResponse(response, data) {
  setResponseText(JSON.stringify({
    status: response.status,
    ok: response.ok,
    body: data
  }, null, 2));
}

function normalizeAvatarList(data) {
  if (Array.isArray(data)) {
    return data;
  }

  if (data && Array.isArray(data.avatars)) {
    return data.avatars;
  }

  if (data && Array.isArray(data.items)) {
    return data.items;
  }

  return [];
}

function activateTab(name) {
  tabs.forEach((tab) => {
    tab.classList.toggle('is-active', tab.dataset.tab === name);
  });

  document.querySelector('#galleryPanel').classList.toggle('is-active', name === 'gallery');
  document.querySelector('#responsePanel').classList.toggle('is-active', name === 'response');
}

function setUploadLoading(isLoading) {
  uploadButton.disabled = isLoading;
  uploadButton.textContent = isLoading ? 'Загрузка' : 'Загрузить';
}

function setResolveLoading(isLoading) {
  resolveUserButton.disabled = isLoading;
  resolveGalleryButton.disabled = isLoading;
  resolveUserButton.textContent = isLoading ? 'Получение' : 'Получить UUID';
}

function showNotice(message, type) {
  uploadNotice.textContent = message;
  uploadNotice.className = `notice is-visible ${type === 'success' ? 'notice--success' : 'notice--error'}`;
}

function hideNotice() {
  uploadNotice.className = 'notice';
  uploadNotice.textContent = '';
}

function clearPreview() {
  preview.classList.remove('is-visible');
  previewImage.removeAttribute('src');
  previewName.textContent = '';
  previewMeta.textContent = '';
}

function renderEmptyState(message) {
  avatarList.innerHTML = '';

  const element = document.createElement('div');
  element.className = 'empty-state';
  element.textContent = message;
  avatarList.append(element);
}

function setResponseText(value) {
  responseOutput.textContent = value;
}

function formatBytes(bytes) {
  if (bytes < 1024) {
    return `${bytes} B`;
  }

  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }

  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
