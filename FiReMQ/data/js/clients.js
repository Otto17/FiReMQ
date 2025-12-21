// КОНТЕКСТНОЕ МЕНЮ

let currentClientID = null;
let selectedContextRow = null;

// Проверяем, есть ли хотя бы один чекбокс со значением true в "sessionStorage"
function hasCheckedClients() {
  return Object.values(checkboxStates).some((state) => state === true);
}


function hideContextMenu() {
  const cm = document.getElementById("contextMenu");
  if (cm.style.display === "block") {
    cm.style.display = "none";
    // Убираем выделение строки
    if (selectedContextRow) {
      selectedContextRow.classList.remove("selected-context");
      selectedContextRow = null;
    }
  }
}

// Показ контекстного меню
function showContextMenu(event, clientID) {
  event.preventDefault();
  currentClientID = clientID;

// Снимаем предыдущее выделение, если было
  if (selectedContextRow) {
    selectedContextRow.classList.remove("selected-context");
  }

  // Находим строку для выделения и добавляем класс
  const row = event.target.closest(`tr[data-id="${clientID}"]`);
  if (row) {
    row.classList.add("selected-context");
    selectedContextRow = row;
  }

  // Получаем ссылки на элементы меню
  const takeOffAllItem = document.getElementById("takeOffAll");
  const moveClientItem = document.getElementById("moveClient");
  const moveClientCheckedItem = document.getElementById("moveClientCheckedItem");
  const renameItem = document.getElementById("renameClient");
  const deleteItem = document.getElementById("deleteItem");
  const deleteCheckedItem = document.getElementById("deleteCheckedItem");
  const runCommandItem = document.getElementById("runCommandItem");
  const installProgramItem = document.getElementById("installProgramItem");
  const informLiteClientItem = document.getElementById("informLiteClient");
  const informAidaClientItem = document.getElementById("informAidaClient");

  // Отображаем только один из пунктов "Удалить" или "Удалить выделенных"
  if (deleteItem && deleteCheckedItem) {
    deleteItem.classList.toggle("hidden", hasCheckedClients());
	deleteCheckedItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Отображаем только один из пунктов "Переместить в..." или "Переместить выделенных"
  if (moveClientItem && moveClientCheckedItem) {
	moveClientItem.classList.toggle("hidden", hasCheckedClients());
	moveClientCheckedItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Отображаем или скрываем пункты "Информация Lite (F1)" и "Информация Aida64 (F4)"
  if (informLiteClientItem) {
    informLiteClientItem.classList.toggle("hidden", hasCheckedClients());
	informAidaClientItem.classList.toggle("hidden", hasCheckedClients());
  }

  // Отображаем или скрываем "Переименовать (F2)"
  if (renameItem) {
    renameItem.classList.toggle("hidden", hasCheckedClients());
  }

  // Отображаем или скрываем "Снять все '✓'"
  if (takeOffAllItem) {
    takeOffAllItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Отображаем или скрываем "Выполнить cmd / PowerShell"
  if (runCommandItem) {
    runCommandItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Отображаем или скрываем "Установка ПО"
  if (installProgramItem) {
    installProgramItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Позиционируем и отображаем контекстное меню
  const contextMenu = document.getElementById("contextMenu");
  contextMenu.style.display = "block";
  const menuWidth = contextMenu.offsetWidth;
  const menuHeight = contextMenu.offsetHeight;

  // Получаем размеры видимой области и текущую прокрутку
  const windowWidth = document.documentElement.clientWidth;
  const windowHeight = document.documentElement.clientHeight;
  const scrollX = window.scrollX;
  const scrollY = window.scrollY;
  const maxX = scrollX + windowWidth;
  const maxY = scrollY + windowHeight;

  // Начальные координаты из события
  let left = event.pageX;
  let top = event.pageY;

  // Корректировка по правой границе
  if (left + menuWidth > maxX) {
    left = maxX - menuWidth - 5;
  }

  // Корректировка по нижней границе
  if (top + menuHeight > maxY) {
    top = maxY - menuHeight - 5;
  }

  // Убедимся, что меню не выходит за левую и верхнюю границы viewport
  left = Math.max(scrollX, left);
  top = Math.max(scrollY, top);

  // Устанавливаем позицию
  contextMenu.style.left = `${left}px`;
  contextMenu.style.top = `${top}px`;
}

// Закрытие контекстного меню при клике вне его
document.addEventListener("click", function (e) {
  const cm = document.getElementById("contextMenu");
  if (cm.style.display === "block" && !cm.contains(e.target)) {
    hideContextMenu();
  }
});

// Закрытие контекстного меню при нажатии клавиши Esc
document.addEventListener("keydown", function (event) {
  if (event.key === "Escape") {
    hideContextMenu();
  }
});

// Закрываем меню и сбрасываем подсветку сразу после клика по любому пункту
document.getElementById("contextMenu").addEventListener("click", function (e) {
  // если клик по <li class="context-menu-item">
  if (e.target.closest(".context-menu-item")) {
    hideContextMenu();
  }
});

// Привязка обработчиков для контекстного меню
document.addEventListener("DOMContentLoaded", function() {
  // Перемещение клиента
  const moveClientBtn = document.getElementById("moveClient");
  if (moveClientBtn) {
    moveClientBtn.addEventListener("click", () => {
      loadExistingGroups();
      document.getElementById("moveClientModal").style.display = "flex";
    });
  }

  // Перемещение выделенных клиентов
  const moveClientCheckedBtn = document.getElementById("moveClientCheckedItem");
  if (moveClientCheckedBtn) {
    moveClientCheckedBtn.addEventListener("click", moveClientCheckClient);
  }

  // Установка ПО
  const installProgramBtn = document.getElementById("installProgramItem");
  if (installProgramBtn) {
    installProgramBtn.addEventListener("click", installProgram);
  }

  // Выполнение команд
  const runCommandBtn = document.getElementById("runCommandItem");
  if (runCommandBtn) {
    runCommandBtn.addEventListener("click", runCommand);
  }

  // Информация Lite
  const informLiteBtn = document.getElementById("informLiteClient");
  if (informLiteBtn) {
    informLiteBtn.addEventListener("click", informLiteClient);
  }

  // Информация Aida64
  const informAidaBtn = document.getElementById("informAidaClient");
  if (informAidaBtn) {
    informAidaBtn.addEventListener("click", informAidaClient);
  }

  // Переименовать
  const renameBtn = document.getElementById("renameClient");
  if (renameBtn) {
    renameBtn.addEventListener("click", renameClient);
  }

  // Удалить
  const deleteBtn = document.getElementById("deleteItem");
  if (deleteBtn) {
    deleteBtn.addEventListener("click", deleteClient);
  }

  // Удалить выделенных
  const deleteCheckedBtn = document.getElementById("deleteCheckedItem");
  if (deleteCheckedBtn) {
    deleteCheckedBtn.addEventListener("click", deleteCheckClient);
  }

  // Снять все галочки
  const takeOffAllBtn = document.getElementById("takeOffAll");
  if (takeOffAllBtn) {
    takeOffAllBtn.addEventListener("click", takeOffAll);
  }
});



// ПЕРЕНАЗНАЧЕНИЕ ГОРЯЧИХ КЛАВИШ

(function () {
  // Изоляция "hoveredClientID" для защиты от возможных изменений в других частях кода

  // Переменная для хранения текущего выделенного (под курсором) клиента
  let hoveredClientID = null;

  // Отслеживание клиента, над которым находится курсор
  document.addEventListener("mousemove", function (event) {
    // Определяем строку таблицы под курсором
    const clientRow = event.target.closest("tr[data-id]");
    if (clientRow) {
      hoveredClientID = clientRow.getAttribute("data-id"); // Сохраняем ID клиента
    } else {
      hoveredClientID = null; // Если не над клиентом, сбрасываем
    }
  });

  // Обработчик нажатия горячих клавиш
  document.addEventListener("keydown", function (event) {
    // Обработчик для горячей клавиши F1
    if (event.key === "F1") {
      event.preventDefault(); // Предотвращаем стандартное поведение F1
      if (hoveredClientID) {
        openClientInfoInNewTab(hoveredClientID, "Lite_"); // Открываем в новой вкладке файл для клиента под курсором
      }
      // Обработчик для горячей клавиши F2
    } else if (event.key === "F2") {
      event.preventDefault(); // Предотвращаем стандартное поведение F2
      if (hoveredClientID) {
        enableEdit(hoveredClientID); // Включаем режим редактирования для клиента
      }
      // Обработчик для горячей клавиши F4
    } else if (event.key === "F4") {
      event.preventDefault(); // Предотвращаем стандартное поведение F4
      if (hoveredClientID) {
        openClientInfoInNewTab(hoveredClientID, "Aida_"); // Включаем режим редактирования для клиента
      }
      // Обработчик для горячей клавиши Esc
    } else if (event.key === "Escape") {
      event.preventDefault(); // Предотвращаем стандартное поведение Esc
      if (hoveredClientID) {
        cancelEdit(hoveredClientID); // Отменяем редактирование клиента под курсором
      }

      // Обработчик для горячей клавиши Enter
    } else if (event.key === "Enter") {
      event.preventDefault(); // Предотвращает стандартное поведение Enter
      if (hoveredClientID) {
        const input = document.getElementById("nameInput_" + hoveredClientID);
        const display = document.getElementById("nameDisplay_" + hoveredClientID);
		
        if (input && display && display.classList.contains("hidden")) {
            saveName(hoveredClientID); // Сохраняем имя при активном поле ввода
        }
      }
    }
  });
})();



// СОРТИРОВКА КЛИЕНТОВ

// Сортировка столбцов
let sortDirections = {
  status: true,
  name: true,
  ip: true,
  local_ip: true,
  client_id: true,
  timestamp: true,
};

// Функция для сортировки таблицы по заданному полю
// Функция для сортировки таблицы по заданному полю
function sortTable(field, forceDirectionChange = true) {
  const table = document.querySelector(".clients table");
  if (!table) return;

  // Переключаем направление ТОЛЬКО по клику пользователя
  if (forceDirectionChange) {
    sortDirections[field] = !sortDirections[field];
  }

  const isAsc = !!sortDirections[field]; // true = ▲ (возрастание)
  const dir = isAsc ? 1 : -1;

  const tbody = table.tBodies[0];
  const rows = Array.from(tbody.rows);

  rows.sort((a, b) => {
    let valA, valB;

    if (field === "status") {
      // "On" выше "Off" при isAsc = true
      const sA = a.querySelector(`[data-field="${field}"] .status-text`)?.textContent || "";
      const sB = b.querySelector(`[data-field="${field}"] .status-text`)?.textContent || "";
      const offA = sA === "Off" ? 1 : 0;
      const offB = sB === "Off" ? 1 : 0;
      return dir * (offA - offB);
    } else if (field === "timestamp") {
      const tA = parseDate(a.querySelector(`[data-field="${field}"]`)?.textContent || "");
      const tB = parseDate(b.querySelector(`[data-field="${field}"]`)?.textContent || "");
      // null (если не распарсилось) отправляем вниз при возрастании
      if (!tA && !tB) return 0;
      if (!tA) return dir * 1;
      if (!tB) return dir * -1;
      return dir * (tA - tB);
    } else {
      // Строки
      valA = (a.querySelector(`[data-field="${field}"]`)?.textContent || "").trim();
      valB = (b.querySelector(`[data-field="${field}"]`)?.textContent || "").trim();
      return dir * valA.localeCompare(valB, "ru");
    }
  });

  rows.forEach((row) => tbody.appendChild(row));

  // Сохраняем текущее (уже применённое) состояние
  localStorage.setItem("sortField", field);
  localStorage.setItem("sortDirection", String(isAsc));

  // Обновляем индикатор именно по текущему направлению
  updateSortIndicator(field, isAsc);
}

// Обновление индикатора сортировки
function updateSortIndicator(field, direction) {
  // Сбрасываем все стрелки
  document.querySelectorAll(".sort-indicator").forEach((indicator) => {
    indicator.classList.remove("active");
    indicator.innerText = "";
  });

  // Устанавливаем стрелку для текущего поля
  let indicator = document.getElementById("sortIndicator_" + field);
  if (indicator) {
    indicator.classList.add("active");
    indicator.innerText = direction ? "▲" : "▼"; // Определяем направление
  }
}

// Конвертация IP в число для корректной сортировки
function ipToNumber(ip) {
  return ip.split(".").reduce((acc, octet) => (acc << 8) + parseInt(octet), 0);
}

// Конвертация даты из формата "дд.мм.гг(чч:мм)" в объект Date
function parseDate(dateStr) {
  // Проверяем корректность строки с помощью регулярного выражения
  const match = /^(\d{2})\.(\d{2})\.(\d{2})\((\d{2}):(\d{2})\)$/.exec(dateStr);
  if (!match) return null; // Если формат не соответствует, возвращаем null

  const [, day, month, year, hours, minutes] = match;
  return new Date(`20${year}`, month - 1, day, hours, minutes);
}



// ГАЛОЧКИ ДЛЯ ВЫБОРА КЛИЕНТОВ

// Выделить "Все"
let allChecked = false;

// Глобальный объект для хранения состояний чекбоксов
let checkboxStates = JSON.parse(sessionStorage.getItem("checkboxStates")) || {};

// Функция для сохранения состояния чекбоксов в глобальный объект и sessionStorage
function saveCheckboxStates() {
  const checkboxes = document.querySelectorAll("input[type='checkbox'][id^='checkbox_']");
  checkboxes.forEach((checkbox) => {
    checkboxStates[checkbox.id] = checkbox.checked;
  });
  sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates));
}

// Функция для восстановления состояний чекбоксов из глобального объекта
function restoreCheckboxStates() {
  const checkboxes = document.querySelectorAll("input[type='checkbox'][id^='checkbox_']");
  checkboxes.forEach((checkbox) => {
    if (checkboxStates[checkbox.id] !== undefined) {
      checkbox.checked = checkboxStates[checkbox.id];
    }
  });
}

// Функция для выделения всех чекбоксов
function toggleAllCheckboxes() {
  const checkboxes = document.querySelectorAll("input[type='checkbox'][id^='checkbox_']");

  // Проверяем, есть ли хотя бы один невыбранный чекбокс
  const anyUnchecked = Array.from(checkboxes).some((checkbox) => !checkbox.checked);

  // Устанавливаем новое состояние в зависимости от текущего
  allChecked = anyUnchecked; // Если есть невыбранные — включаем все, иначе — выключаем

  checkboxes.forEach((checkbox) => {
    checkbox.checked = allChecked;
    checkboxStates[checkbox.id] = allChecked; // Обновляем состояния в глобальном объекте
  });
  sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates)); // Сохраняем в sessionStorage
}

// Снять все '✓'
function takeOffAll() {
  // Пробегаемся по всем сохранённым состояниям чекбоксов
  for (const clientId in checkboxStates) {
    if (checkboxStates.hasOwnProperty(clientId)) {
      checkboxStates[clientId] = false; // Устанавливаем состояние в false
    }
  }

  // Сохраняем изменения в "sessionStorage"
  sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates));

  // Снимаем галочки только для видимых чекбоксов на странице
  const checkboxes = document.querySelectorAll("input[type='checkbox'][id^='checkbox_']");
  checkboxes.forEach((checkbox) => {
    checkbox.checked = false;
  });

  // Сбрасываем флаг allChecked, так как все галочки сняты
  allChecked = false;

  // Обновляем состояние кнопок "Установка ПО" и "Выполнить cmd / PowerShell"
  if (typeof updateClientActionButtons === "function") {
    updateClientActionButtons();
  }
}

// Функция выделения при клике в любом месте на ячейке с чекбоксом
function setupCheckboxCells() {
  const checkboxCells = document.querySelectorAll("td input[type='checkbox'][id^='checkbox_']");

  checkboxCells.forEach((checkbox) => {
    // Получаем родительскую ячейку <td>
    const cell = checkbox.parentElement;

    cell.addEventListener("click", (event) => {
      // Проверяем, что клик был не на самом чекбоксе
      if (event.target !== checkbox) {
        checkbox.checked = !checkbox.checked; // Переключаем состояние чекбокса
        checkboxStates[checkbox.id] = checkbox.checked; // Обновляем состояния
        sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates)); // Сохраняем в sessionStorage

        if (typeof updateClientActionButtons === "function") {
          // Проверяем наличие функции
          updateClientActionButtons(); // Обновляем кнопку
        }
      }
    });

    // Обработчик для изменения состояния чекбокса
    checkbox.addEventListener("change", () => {
      checkboxStates[checkbox.id] = checkbox.checked;
      sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates)); // Сохраняем изменения

      if (typeof updateClientActionButtons === "function") {
        // Проверяем наличие функции
        updateClientActionButtons(); // Обновляем кнопку
      }
    });
  });

  // Настройка ячейки "Все"
  const allCheckboxCell = document.querySelector("th[data-field='all']");
  if (allCheckboxCell) {
    allCheckboxCell.addEventListener("click", () => {
      toggleAllCheckboxes();
      if (typeof updateClientActionButtons === "function") {
        // Проверяем наличие функции
        updateClientActionButtons(); // Обновляем кнопку
      }
    });
  }
}



// ПЕРЕИМЕНОВАНИЕ КЛИЕНТА

//Функция для контекстного меню
function renameClient() {
  enableEdit(currentClientID);
}

// Функция для включения режима редактирования
function enableEdit(clientID) {
  const input = document.getElementById("nameInput_" + clientID);
  const display = document.getElementById("nameDisplay_" + clientID);

  if (input && display) {
    display.classList.add("hidden");
    input.classList.remove("hidden");
	input.style.display = "inline"; // Явно показываем input
    input.focus();

    // Ограничение длины ввода
    input.setAttribute("maxlength", "80");

    // Добавляем placeholder
    input.setAttribute("placeholder", "Новое имя (до 80)");
  } else {
    console.error("Элементы для редактирования клиента не найдены: " + clientID);
  }
}

// Функция отмены редактирования
function cancelEdit(clientID) {
  const input = document.getElementById("nameInput_" + clientID);
  const display = document.getElementById("nameDisplay_" + clientID);

  if (input && display && display.classList.contains("hidden")) {
    display.classList.remove("hidden");
    input.style.display = "";
  }
}

// Сохранение нового имени
function saveName(clientID) {
  const input = document.getElementById("nameInput_" + clientID);
  const display = document.getElementById("nameDisplay_" + clientID);
  const newName = input ? input.value.trim() : "";
  const oldName = display ? display.innerText.trim() : "";

  // Проверка, совпадает ли новое имя со старым
  if (newName === oldName) {
    cancelEdit(clientID);
    showPush("Имя клиента не изменено", "#ff4081"); // Розовый
    return;
  }

  // Отправляем на сервер, только если имя изменено
  apiPostJson("/set-name-client", { clientID: clientID, name: newName })
    .then((response) => {
      if (!response.ok) {
        return response.text().then((errorText) => {
          throw new Error(errorText || "Ошибка сохранения имени клиента");
        });
      }
      return response.text();
    })
    .then((data) => {
      if (display) {
        display.innerText = newName;
        cancelEdit(clientID);
        showPush(data, "#2196F3"); // Голубой
      }
    })
    .catch((error) => {
      showPush(error.message, "#ff4d4d"); // Красный
    });
}



// ОТКРЫТИЕ ИЛИ СКАЧИВАНИЕ ФАЙЛА С ИНФОРМАЦИЕЙ О КЛИЕНТЕ

// Открытие отчёта в новой вкладке (POST -> временная ссылка)
async function openClientInfoInNewTab(clientID, prefix) {
  try {
    const response = await apiPostJson('/getFile-info', {
      clientID,
      prefix,
      action: 'view',
    });

    if (!response.ok) {
      if (response.status === 404) {
        showPush(`Архив ${prefix}${clientID} не найден`, '#ff4d4d'); // Красный
      } else {
        const text = await response.text().catch(() => '');
        showPush(`Ошибка ${response.status}: ${response.statusText}${text ? ' — ' + text : ''}`, '#ff4d4d'); // Красный
      }
      return;
    }

    const data = await response.json();
    const url = data.reportURL;
    if (url) {
      window.open(url, '_blank', 'noopener,noreferrer');
      showPush('Отчёт открыт в новой вкладке', '#2196F3'); // Голубой
    } else {
      showPush('Не удалось получить ссылку на отчёт', '#ff4d4d'); // Красный
    }
  } catch (err) {
    showPush(`Ошибка сети: ${err.message}`, '#ff4d4d'); // Красный
  }
}

// Функция для обработки клика на пункте "Информация Lite (F1)"
function informLiteClient() {
  if (currentClientID) {
    openClientInfoInNewTab(currentClientID, "Lite_");
  } else {
    showPush("Клиент не выбран", "#ff4d4d"); // Красный
  }
}

// Функция для обработки клика на пункте "Информация Aida64 (F4)"
function informAidaClient() {
  if (currentClientID) {
    openClientInfoInNewTab(currentClientID, "Aida_");
  } else {
    showPush("Клиент не выбран", "#ff4d4d"); // Красный
  }
}



// ДИНАМИЧЕСКИЕ КЛИЕНТЫ

// Функция проверки браузера
function isFirefoxBrowser() {
  return navigator.userAgent.toLowerCase().includes("firefox");
}

// Функция загрузки клиентов после выбора группы или подгруппы
function loadClients(group, subgroup) {
  // Сохраняем текущие состояния чекбоксов
  saveCheckboxStates();

  let url = "/get-clients-by-group";
  if (group) {
    url += "?group=" + encodeURIComponent(group);
    if (subgroup) {
      url += "&subgroup=" + encodeURIComponent(subgroup);
    }
  }
  fetch(url)
    .then((response) => response.json())
    .then((data) => {
      const clientsContainer = document.getElementById("clientsContainer");
      clientsContainer.innerHTML = ""; // Очищаем текущее содержимое контейнера клиентов

      const table = document.createElement("table");
      const thead = document.createElement("thead");
      const tbody = document.createElement("tbody");

      // Применяем стили таблицы в зависимости от браузера
      if (isFirefoxBrowser()) {
        table.style.borderSpacing = "0";
        table.style.borderCollapse = ""; // Убираем, чтобы не конфликтовало
      } else {
        table.style.borderCollapse = "collapse";
        table.style.borderSpacing = ""; // Убираем, чтобы не конфликтовало
      }

      // Создаем заголовок таблицы
      const headerRow = document.createElement("tr");
      const headers = [
        {
          field: "status",
          text: "Статус",
          sortable: true,
        },
        {
          field: "name",
          text: "Имя",
          sortable: true,
        },
        {
          field: "ip",
          text: "IP",
          sortable: true,
        },
        {
          field: "local_ip",
          text: "Серый IP",
          sortable: true,
        },
        {
          field: "client_id",
          text: "ID Клиента",
          sortable: true,
        },
        {
          field: "timestamp",
          text: "От",
          sortable: true,
        },
        {
          field: "all",
          text: "Все",
          sortable: false,
        },
      ];

      headers.forEach((header) => {
        const th = document.createElement("th");
        th.textContent = header.text;
        if (header.sortable) {
          th.onclick = () => sortTable(header.field);
          th.innerHTML += `<span id="sortIndicator_${header.field}" class="sort-indicator"></span>`;
        }
        if (header.field === "all") {
          th.setAttribute("data-field", "all");
        }
        headerRow.appendChild(th);
      });

      thead.appendChild(headerRow);
      table.appendChild(thead);

      // Создаем строки таблицы для каждого клиента
      data.forEach((client) => {
        const checkboxId = `checkbox_${client.ClientID}`;
        const newRow = document.createElement("tr");

        // Добавляем data-id в строку для каждого клиента
        newRow.setAttribute("data-id", client.ClientID);
        newRow.innerHTML = `
			<td data-field="status">
			  <span class="status-text hidden">${client.Status}</span>
			  <img class="status-image" src="${client.Status === "On" ? "../icon/PC_On.svg" : "../icon/PC_Off.svg"}" alt="${client.Status}">
			</td>
			<td data-field="name">
			  <span id="nameDisplay_${client.ClientID}">${client.Name}</span>
			  <input id="nameInput_${client.ClientID}" type="text"
			  value="${client.Name}" class="name-input hidden">
			</td>
			<td data-field="ip">${client.IP}</td>
			<td data-field="local_ip">${client.LocalIP}</td>
			<td data-field="client_id">${client.ClientID}</td>
			<td data-field="timestamp">${client.Timestamp}</td>
			<td>
			  <input type="checkbox" name="${checkboxId}" id="${checkboxId}">
			</td>
			`;

        // Привязка контекстного меню к строке клиента
        newRow.addEventListener("contextmenu", (event) => showContextMenu(event, client.ClientID));

        tbody.appendChild(newRow);
      });

      table.appendChild(tbody);
      clientsContainer.appendChild(table);

      // Настройка ячеек с чекбоксами после загрузки клиентов
      setupCheckboxCells();

      // Восстанавливаем состояния чекбоксов
      restoreCheckboxStates();

      // Добавляем обработчик для сохранения состояния при изменении чекбокса
      const checkboxes = document.querySelectorAll("input[type='checkbox'][id^='checkbox_']");
      checkboxes.forEach((checkbox) => {
        checkbox.addEventListener("change", saveCheckboxStates);
      });

      // Восстанавливаем состояние сортировки из localStorage (без переключения)
		const savedField = localStorage.getItem("sortField");
		const savedDirectionStr = localStorage.getItem("sortDirection");

		if (savedField !== null && savedDirectionStr !== null) {
		  sortDirections[savedField] = (savedDirectionStr === "true"); // true = ▲
		  sortTable(savedField, false); // применяем без инверсии
		} else {
		  // Дефолт: сортировка по имени по возрастанию (▲), без инверсии
		  sortDirections["name"] = true;
		  sortTable("name", false);
		}
    })
    .catch((error) => {
      console.error("Ошибка при загрузке данных:", error);
    });
}
