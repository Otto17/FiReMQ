// Кнопка "Скачать отчёт" на странице отчёта

async function downloadFile() {
  try {
    const body = document.body;
    const clientID = body.dataset.clientId;
    const prefix = body.dataset.prefix;

    if (!clientID || !prefix) {
	  showPush("Ошибка: отсутствуют параметры отчёта.", "#ff4d4d"); // Красный
      return;
    }

    const response = await apiPostJson('/getFile-info', {
      clientID,
      prefix,
      action: 'download',
    });

    if (!response.ok) {
      const text = await response.text().catch(() => '');
      throw new Error(`HTTP ${response.status} ${response.statusText}${text ? ' — ' + text : ''}`);
    }

    const blob = await response.blob();
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${prefix}${clientID}.html`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    window.URL.revokeObjectURL(url);
  } catch (error) {
	showPush("Ошибка при скачивании файла: " + error.message, "#ff4d4d"); // Красный
  }
}

// Привязка обработчика к кнопке без inline JS
document.addEventListener('DOMContentLoaded', () => {
  const btn = document.getElementById('downloadBtn');
  if (btn) btn.addEventListener('click', downloadFile);
});