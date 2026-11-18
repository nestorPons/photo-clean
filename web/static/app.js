// photo-clean — comportamiento mínimo del formulario.
// HTMX gestiona la subida y el polling; aquí solo drag & drop, el nombre del
// archivo y el estado "subiendo → procesando".
(() => {
  const input = document.getElementById("zip");
  const zone = document.getElementById("dropzone");
  const fileName = document.getElementById("file-name");
  if (!input || !zone || !fileName) return;

  const form = document.getElementById("upload");
  const statusText = document.getElementById("upload-status-text");
  const defaultText = fileName.textContent;

  const setStatus = (text) => {
    if (statusText) statusText.textContent = text;
  };

  const showFile = () => {
    if (!input.files || input.files.length === 0) {
      fileName.textContent = defaultText;
      zone.classList.remove("has-file");
      return;
    }
    const file = input.files[0];
    const mb = (file.size / (1024 * 1024)).toFixed(1);
    fileName.textContent = `${file.name} · ${mb} MB`;
    zone.classList.add("has-file");
  };

  input.addEventListener("change", showFile);

  ["dragenter", "dragover"].forEach((event) => {
    zone.addEventListener(event, (e) => {
      e.preventDefault();
      zone.classList.add("dragover");
    });
  });

  ["dragleave", "dragend", "drop"].forEach((event) => {
    zone.addEventListener(event, (e) => {
      e.preventDefault();
      zone.classList.remove("dragover");
    });
  });

  zone.addEventListener("drop", (e) => {
    const files = e.dataTransfer && e.dataTransfer.files;
    if (!files || files.length === 0) return;
    input.files = files;
    showFile();
  });

  // Solo nos interesan los eventos del formulario de subida: el polling de
  // progreso también lanza peticiones y no debe tocar este estado.
  const isUpload = (e) => !form || (e.detail && e.detail.elt === form);

  // Desde el primer segundo: subida con porcentaje real.
  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (isUpload(e)) setStatus("Subiendo ZIP…");
  });

  document.body.addEventListener("htmx:xhr:progress", (e) => {
    if (!isUpload(e)) return;
    const { lengthComputable, loaded, total } = e.detail || {};
    if (!lengthComputable || !total) return;
    const percent = Math.round((loaded / total) * 100);
    setStatus(percent >= 100 ? "ZIP subido. Procesando imágenes…" : `Subiendo ZIP… ${percent} %`);
  });

  // La subida terminó y el servidor está validando/creando el trabajo.
  document.body.addEventListener("htmx:xhr:loadend", (e) => {
    if (isUpload(e)) setStatus("ZIP subido. Procesando imágenes…");
  });

  // Tras la respuesta, el panel de progreso ya está en el DOM: llévalo a la vista.
  document.body.addEventListener("htmx:afterRequest", (e) => {
    if (!isUpload(e)) return;
    zone.classList.remove("has-file", "dragover");
    if (!e.detail || !e.detail.successful) return;

    // Solo limpiamos el formulario si el servidor creó un trabajo; si hubo un
    // error de subida, el usuario puede corregir sin volver a elegir el ZIP.
    const body = (e.detail.xhr && e.detail.xhr.responseText) || "";
    if (/hx-get="\/jobs\//.test(body)) {
      input.value = "";
      fileName.textContent = defaultText;
    }

    const panel = document.getElementById("job-panel");
    if (panel) requestAnimationFrame(() => panel.scrollIntoView({ behavior: "smooth", block: "nearest" }));
  });
})();
