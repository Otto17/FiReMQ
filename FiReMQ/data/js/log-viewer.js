// Кнопка "Скачать лог" в Меню -> Логи

document.addEventListener('DOMContentLoaded', () => {
    // Поиск панели и родительского контейнера
    const filtersPanel = document.querySelector('.filters');
    const topRow = document.querySelector('.top-row'); 
    
    if (!filtersPanel || !topRow) return;

    // Создание кнопки
    const downloadBtn = document.createElement('button');
    downloadBtn.textContent = 'Скачать лог';
    
    // Стилизация кнопки "Скачать лог"
    downloadBtn.style.fontSize = '14px';
    downloadBtn.style.padding = '10px 20px';
    downloadBtn.style.backgroundColor = '#ff2b1b'; // Красная кнопка
    downloadBtn.style.color = 'white';
    downloadBtn.style.border = 'none';
    downloadBtn.style.borderRadius = '4px';
    downloadBtn.style.cursor = 'pointer';

    // Абсолютное позиционирование (справа по центру строки)
    downloadBtn.style.position = 'absolute';
    downloadBtn.style.right = '0';
    downloadBtn.style.top = '50%';
    downloadBtn.style.transform = 'translateY(-50%)';

    // Добавление кнопки
    topRow.appendChild(downloadBtn); 

    // Логика скачивания
    downloadBtn.addEventListener('click', async () => {
        try {
            // Получение CSRF токена
            const csrfResp = await fetch('/csrf-token');
            if (!csrfResp.ok) throw new Error('Auth Error');
            const { csrf_token } = await csrfResp.json();

            // Запрос файла лога
            const response = await fetch('/getServer-log', {
                method: 'POST',
                headers: { 
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': csrf_token
                },
                body: JSON.stringify({ action: 'download' })
            });

             if (!response.ok) {
                // showToast определён в htmlHeader, в файле сервера "log.go"
                if (typeof showToast === 'function') {
                    showToast("Ошибка при скачивании (код " + response.status + ")");
                } else {
                    alert("Ошибка при скачивании (код " + response.status + ")");
                }
                return;
            }

            // Получение файла и инициирование его скачивания
            const blob = await response.blob();
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = 'FiReMQ_Logs.html';
            document.body.appendChild(a);
            a.click();
            a.remove();
            window.URL.revokeObjectURL(url);
        } catch (e) {
            console.error(e);
            if (typeof showToast === 'function') {
                showToast("Ошибка сети или авторизации при скачивании: " + e.message);
            } else {
                alert("Ошибка сети или авторизации при скачивании: " + e.message);
            }
        }
    });
});