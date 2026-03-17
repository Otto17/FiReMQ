// КОНТЕКСТНОЕ МЕНЮ

let currentClientID = null;
let selectedContextRow = null;

// Обновляет заголовок "Группы" с количеством клиентов в выбранной группе/подгруппе
function updateGroupsHeader(isSubgroup) {
  const header = document.getElementById("groupsHeader");
  if (!header) return;

  // Считает количество клиентов прямо из отрисованной таблицы
  const tbody = document.querySelector(".clients table tbody");
  if (tbody && tbody.rows.length > 0) {
    const label = isSubgroup ? "В подгруппе" : "В группе";
    header.textContent = `${label} (${tbody.rows.length})`;
  } else {
    header.textContent = "Группы";
  }
}

// Обновляет заголовок "Клиенты" с подсчётом выделенных галочками
function updateClientsHeader() {
  const header = document.getElementById("clientsHeader");
  if (!header) return;

  // Подсчёт выделенных галочками клиентов (по всем группам)
  const checkedCount = Object.values(checkboxStates).filter(v => v === true).length;

  if (checkedCount > 0) {
    header.textContent = `Клиенты (выделено: ${checkedCount})`;
  } else {
    header.textContent = "Клиенты";
  }
}

// Проверяет, есть ли хотя бы один чекбокс со значением true в "sessionStorage"
function hasCheckedClients() {
  return Object.values(checkboxStates).some((state) => state === true);
}


function hideContextMenu() {
  const cm = document.getElementById("contextMenu");
  if (cm.style.display === "block") {
    cm.style.display = "none";
    // Убирает выделение строки
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

  // Снимает предыдущее выделение, если было
  if (selectedContextRow) {
    selectedContextRow.classList.remove("selected-context");
  }

  // Находим строку для выделения и добавляет класс
  const row = event.target.closest(`tr[data-id="${clientID}"]`);
  if (row) {
    row.classList.add("selected-context");
    selectedContextRow = row;
  }

  // Получает ссылки на элементы меню
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

  // Отображает только один из пунктов "Удалить" или "Удалить выделенных"
  if (deleteItem && deleteCheckedItem) {
    deleteItem.classList.toggle("hidden", hasCheckedClients());
    deleteCheckedItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Отображает только один из пунктов "Переместить в..." или "Переместить выделенных"
  if (moveClientItem && moveClientCheckedItem) {
    moveClientItem.classList.toggle("hidden", hasCheckedClients());
    moveClientCheckedItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Отображает или скрывает пункты "Информация Lite (F1)" и "Информация Aida64 (F4)"
  if (informLiteClientItem) {
    informLiteClientItem.classList.toggle("hidden", hasCheckedClients());
    informAidaClientItem.classList.toggle("hidden", hasCheckedClients());
  }

  // Отображает или скрывает "Переименовать (F2)"
  if (renameItem) {
    renameItem.classList.toggle("hidden", hasCheckedClients());
  }

  // Отображает или скрывает "Снять все '✓'"
  if (takeOffAllItem) {
    takeOffAllItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Отображает или скрывает "Выполнить cmd / PowerShell"
  if (runCommandItem) {
    runCommandItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Отображает или скрывает "Установка ПО"
  if (installProgramItem) {
    installProgramItem.classList.toggle("hidden", !hasCheckedClients());
  }

  // Позиционирует и отображает контекстное меню
  const contextMenu = document.getElementById("contextMenu");
  contextMenu.style.display = "block";
  const menuWidth = contextMenu.offsetWidth;
  const menuHeight = contextMenu.offsetHeight;

  // Получает размеры видимой области и текущую прокрутку
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

  // Устанавливает позицию
  contextMenu.style.left = `${left}px`;
  contextMenu.style.top = `${top}px`;
}

// Закрытие контекстного меню при клике вне его
document.addEventListener("click", function(e) {
  const cm = document.getElementById("contextMenu");
  if (cm.style.display === "block" && !cm.contains(e.target)) {
    hideContextMenu();
  }
});

// Закрытие контекстного меню при нажатии клавиши Esc
document.addEventListener("keydown", function(event) {
  if (event.key === "Escape") {
    hideContextMenu();
  }
});

// Закрывает меню и сбрасывает подсветку сразу после клика по любому пункту
document.getElementById("contextMenu").addEventListener("click", function(e) {
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

(function() {
  // Изоляция "hoveredClientID" для защиты от возможных изменений в других частях кода

  // Переменная для хранения текущего выделенного (под курсором) клиента
  let hoveredClientID = null;

  // Отслеживание клиента, над которым находится курсор
  document.addEventListener("mouseover", function(event) {
    // Определяет строку таблицы под курсором
    const clientRow = event.target.closest("tr[data-id]");
    hoveredClientID = clientRow ? clientRow.getAttribute("data-id") : null;
});

  // Обработчик нажатия горячих клавиш
  document.addEventListener("keydown", function(event) {
    // Обработчик для горячей клавиши F1
    if (event.key === "F1") {
      event.preventDefault(); // Предотвращает стандартное поведение F1
      if (hoveredClientID) {
        openClientInfoInNewTab(hoveredClientID, "Lite_"); // Открывает в новой вкладке файл для клиента под курсором
      }
      // Обработчик для горячей клавиши F2
    } else if (event.key === "F2") {
      event.preventDefault(); // Предотвращает стандартное поведение F2
      if (hoveredClientID) {
        enableEdit(hoveredClientID); // Включает режим редактирования для клиента
      }
      // Обработчик для горячей клавиши F4
    } else if (event.key === "F4") {
      event.preventDefault(); // Предотвращает стандартное поведение F4
      if (hoveredClientID) {
        openClientInfoInNewTab(hoveredClientID, "Aida_"); // Включает режим редактирования для клиента
      }
      // Обработчик для горячей клавиши Esc
    } else if (event.key === "Escape") {
      event.preventDefault(); // Предотвращает стандартное поведение Esc
      if (hoveredClientID) {
        cancelEdit(hoveredClientID); // Отменяет редактирование клиента под курсором
      }

      // Обработчик для горячей клавиши Enter
    } else if (event.key === "Enter") {
      const activeElement = document.activeElement;

      // Проверяет, является ли активный элемент полем ввода имени клиента
      if (activeElement && activeElement.id && activeElement.id.startsWith("nameInput_")) {
        event.preventDefault();
        const clientID = activeElement.id.replace("nameInput_", "");
        saveName(clientID);
      }
    }
  });
})();



// СОРТИРОВКА КЛИЕНТОВ

// Сортировка столбцов
let sortDirections = {
  status: true,
  name: true,
  windows: true,
  ip: true,
  local_ip: true,
  client_id: true,
  timestamp: true,
};

// Кэшированный коллатор для сортировки строк
const ruCollator = new Intl.Collator("ru");

// Функция для сортировки таблицы по заданному полю
function sortTable(field, forceDirectionChange = true) {
  const table = document.querySelector(".clients table");
  if (!table) return;

  // Переключает направление ТОЛЬКО по клику пользователя
  if (forceDirectionChange) {
    sortDirections[field] = !sortDirections[field];
  }

  const isAsc = !!sortDirections[field]; // true = ▲ (возрастание)
  const dir = isAsc ? 1 : -1;

  const tbody = table.tBodies[0];
  const rows = Array.from(tbody.rows);

  // Извлекает значения из DOM один раз
  const cache = new Map();
  for (let i = 0; i < rows.length; i++) {
    const row = rows[i];
    let val;
    if (field === "status") {
      const s = row.querySelector(`[data-field="${field}"] .status-text`)?.textContent || "";
      val = s === "Off" ? 1 : 0;
    } else if (field === "timestamp") {
      val = parseDate(row.querySelector(`[data-field="${field}"]`)?.textContent || "");
    } else {
      val = (row.querySelector(`[data-field="${field}"]`)?.textContent || "").trim();
    }
    cache.set(row, val);
  }

  // Сортировка по кэшированным значениям (без обращений к DOM)
  rows.sort((a, b) => {
    const valA = cache.get(a);
    const valB = cache.get(b);

    if (field === "status") {
      return dir * (valA - valB);
    } else if (field === "timestamp") {
      // null (если не распарсилось) отправляет вниз при возрастании
      if (!valA && !valB) return 0;
      if (!valA) return dir;
      if (!valB) return -dir;
      return dir * (valA - valB);
    } else {
      return dir * ruCollator.compare(valA, valB);
    }
  });

  // Пакетная перестановка строк через DocumentFragment
  const fragment = document.createDocumentFragment();
  for (let i = 0; i < rows.length; i++) {
    fragment.appendChild(rows[i]);
  }
  tbody.appendChild(fragment);

  // Сохраняет текущее (уже применённое) состояние
  localStorage.setItem("sortField", field);
  localStorage.setItem("sortDirection", String(isAsc));

  // Обновляет индикатор по текущему направлению
  updateSortIndicator(field, isAsc);
}

// Обновление индикатора сортировки
function updateSortIndicator(field, direction) {
  // Сбрасывает все стрелки
  document.querySelectorAll(".sort-indicator").forEach((indicator) => {
    indicator.classList.remove("active");
    indicator.innerText = "";
  });

  // Устанавливает стрелку для текущего поля
  let indicator = document.getElementById("sortIndicator_" + field);
  if (indicator) {
    indicator.classList.add("active");
    indicator.innerText = direction ? "▲" : "▼"; // Определяет направление
  }
}

// Конвертация IP в число для корректной сортировки
function ipToNumber(ip) {
  return ip.split(".").reduce((acc, octet) => (acc << 8) + parseInt(octet), 0);
}

// Конвертация даты из формата "дд.мм.гг(чч:мм)" в объект Date
function parseDate(dateStr) {
  // Проверяет корректность строки с помощью регулярного выражения
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

  // Проверяет, есть ли хотя бы один невыбранный чекбокс
  const anyUnchecked = Array.from(checkboxes).some((checkbox) => !checkbox.checked);

  // Устанавливает новое состояние в зависимости от текущего
  allChecked = anyUnchecked; // Если есть невыбранные — включает все, иначе — выключает

  checkboxes.forEach((checkbox) => {
    checkbox.checked = allChecked;
    checkboxStates[checkbox.id] = allChecked; // Обновляет состояния в глобальном объекте
  });
  
  sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates)); // Сохраняет в sessionStorage
  updateClientsHeader();	// Обновляет заголовок с подсчётом клиентов
}

// Снять все '✓'
function takeOffAll() {
  // Проходит по всем сохранённым состояниям чекбоксов
  for (const clientId in checkboxStates) {
    if (checkboxStates.hasOwnProperty(clientId)) {
      checkboxStates[clientId] = false; // Устанавливает состояние в false
    }
  }

  // Сохраняет изменения в "sessionStorage"
  sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates));

  // Снимает галочки только для видимых чекбоксов на странице
  const checkboxes = document.querySelectorAll("input[type='checkbox'][id^='checkbox_']");
  checkboxes.forEach((checkbox) => {
    checkbox.checked = false;
  });

  // Сбрасывает флаг allChecked, так как все галочки сняты
  allChecked = false;

  // Обновляет заголовок с подсчётом клиентов
  updateClientsHeader();

  // Обновляет состояние кнопок "Установка ПО" и "Выполнить cmd / PowerShell"
  if (typeof updateClientActionButtons === "function") {
    updateClientActionButtons();
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
    input.style.display = "inline"; // Явно показывает input
    input.focus();

    // Ограничение длины ввода
    input.setAttribute("maxlength", "80");

    // Добавляет placeholder
    input.setAttribute("placeholder", "Новое имя (до 80)");

    // Добавляет обработчик валидации если ещё не добавлен
    if (!input.dataset.validationInit) {
      input.addEventListener('input', () => updateFieldValidation(input, true));
      input.dataset.validationInit = 'true';
    }

    // Сбрасывает состояние валидации при открытии
    resetFieldValidation(input);
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

    // Сбрасывает состояние валидации
    resetFieldValidation(input);
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

  // Отправляет на сервер, только если имя изменено
  apiPostJson("/set-name-client", {
      clientID: clientID,
      name: newName
    })
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

// Кэш определения браузера Firefox (вычисляется один раз)
const isFirefox = navigator.userAgent.toLowerCase().includes("firefox");

// Обновляет ячейки существующей строки данными клиента (без пересоздания DOM-узлов)
function updateRowCells(row, client) {
  const checkboxId = `checkbox_${client.ClientID}`;
  const statusIcon = client.Status === "On" ? "../icon/PC_On.svg" : "../icon/PC_Off.svg";

  row.setAttribute("data-id", client.ClientID);

  // Статус
  const statusTd = row.children[0];
  const statusSpan = statusTd.children[0];
  const statusImg = statusTd.children[1];
  statusSpan.textContent = client.Status;
  statusImg.src = statusIcon;
  statusImg.alt = client.Status;

  // Имя
  const nameTd = row.children[1];
  const nameDisplay = nameTd.children[0];
  const nameInput = nameTd.children[1];
  nameDisplay.id = "nameDisplay_" + client.ClientID;
  nameDisplay.textContent = client.Name;
  nameInput.id = "nameInput_" + client.ClientID;
  nameInput.value = client.Name;
  // Сброс состояния редактирования
  nameDisplay.classList.remove("hidden");
  nameInput.classList.add("hidden");
  nameInput.style.display = "";

  // Windows
  row.children[2].textContent = client.Windows;

  // IP, Серый IP, ID, Дата
  row.children[3].textContent = client.IP;
  row.children[4].textContent = client.LocalIP;
  row.children[5].textContent = client.ClientID;
  row.children[6].textContent = client.Timestamp;

  // Чекбокс
  const cb = row.children[7].children[0];
  cb.name = checkboxId;
  cb.id = checkboxId;
  cb.checked = false;
}

// HTML-шаблон для создания новой строки клиента
function createRowHTML(client) {
  const checkboxId = `checkbox_${client.ClientID}`;
  const statusIcon = client.Status === "On" ? "../icon/PC_On.svg" : "../icon/PC_Off.svg";
  return `<tr data-id="${client.ClientID}">
		<td data-field="status">
			<span class="status-text hidden">${client.Status}</span>
			<img class="status-image" src="${statusIcon}" alt="${client.Status}">
		</td>
		<td data-field="name">
			<span id="nameDisplay_${client.ClientID}">${client.Name}</span>
			<input id="nameInput_${client.ClientID}" type="text"
				value="${client.Name}" class="name-input hidden">
		</td>
		<td data-field="windows">${client.Windows}</td>
		<td data-field="ip">${client.IP}</td>
		<td data-field="local_ip">${client.LocalIP}</td>
		<td data-field="client_id">${client.ClientID}</td>
		<td data-field="timestamp">${client.Timestamp}</td>
		<td><input type="checkbox" name="${checkboxId}" id="${checkboxId}"></td>
	</tr>`;
}

// Обновляет tbody: переиспользует существующие строки, добавляет недостающие, удаляет лишние
function renderTbodyRows(tbody, data) {
  const existingRows = tbody.rows;
  const existingCount = existingRows.length;
  const newCount = data.length;

  // Переиспользует существующие строки (обновляет ячейки без пересоздания)
  const reuseCount = Math.min(existingCount, newCount);
  for (let i = 0; i < reuseCount; i++) {
    updateRowCells(existingRows[i], data[i]);
  }

  if (newCount > existingCount) {
    // Добавляет недостающие строки через DocumentFragment
    const fragment = document.createDocumentFragment();
    const temp = document.createElement("tbody");
    temp.innerHTML = data.slice(existingCount).map(createRowHTML).join("");
    while (temp.firstChild) {
      fragment.appendChild(temp.firstChild);
    }
    tbody.appendChild(fragment);
  } else if (newCount < existingCount) {
    // Удаляет лишние строки с конца
    for (let i = existingCount - 1; i >= newCount; i--) {
      tbody.removeChild(existingRows[i]);
    }
  }
}

// Маппинг полей сортировки к ключам JSON-ответа
const SORT_FIELD_TO_KEY = {
  status: "Status",
  name: "Name",
  windows: "Windows",
  ip: "IP",
  local_ip: "LocalIP",
  client_id: "ClientID",
  timestamp: "Timestamp",
};

// Сортирует массив клиентов на уровне данных (без обращений к DOM)
function sortClientData(data, field, isAsc) {
  const dir = isAsc ? 1 : -1;
  data.sort((a, b) => {
    if (field === "status") {
      return dir * ((a.Status === "Off" ? 1 : 0) - (b.Status === "Off" ? 1 : 0));
    }
    if (field === "timestamp") {
      const tA = parseDate(a.Timestamp || "");
      const tB = parseDate(b.Timestamp || "");
      if (!tA && !tB) return 0;
      if (!tA) return dir;
      if (!tB) return -dir;
      return dir * (tA - tB);
    }
    const key = SORT_FIELD_TO_KEY[field] || "Name";
    return dir * ruCollator.compare((a[key] || "").trim(), (b[key] || "").trim());
  });
}

// Функция загрузки клиентов после выбора группы или подгруппы
// (таблица, заголовок и обработчики создаются один раз, при переключении заменяется только содержимое tbody,
//  данные сортируются на уровне массива — без DOM-перестановок)
const loadClients = (() => {
  let abortCtrl = null;
  let tableEl = null; // Персистентный элемент таблицы (создаётся один раз)
  let tbodyEl = null; // Персистентный элемент tbody (создаётся один раз)

  return function(group, subgroup) {
    // Сохраняет текущие состояния чекбоксов
    saveCheckboxStates();

    // Отменяет предыдущий незавершённый запрос
    if (abortCtrl) abortCtrl.abort();
    abortCtrl = new AbortController();

    let url = "/get-clients-by-group";
    if (group) {
      url += "?group=" + encodeURIComponent(group);
      if (subgroup) {
        url += "&subgroup=" + encodeURIComponent(subgroup);
      }
    }

    fetch(url, {
        signal: abortCtrl.signal
      })
      .then((response) => response.json())
      .then((data) => {
        // Сбрасывает ссылку на старую строку контекстного меню (предотвращает утечку памяти)
        selectedContextRow = null;

        const clientsContainer = document.getElementById("clientsContainer");

        // Создаёт таблицу, заголовок и обработчики только при первом вызове
        // (или если таблица была удалена из DOM внешним кодом)
        if (!tableEl || !tableEl.parentNode) {
          tableEl = document.createElement("table");
          const thead = document.createElement("thead");
          tbodyEl = document.createElement("tbody");

          // Применяет стили таблицы в зависимости от браузера
          if (isFirefox) {
            tableEl.style.borderSpacing = "0";
          } else {
            tableEl.style.borderCollapse = "collapse";
          }

          // Создаёт заголовок таблицы (один раз)
          const headerRow = document.createElement("tr");
          const headers = [{
              field: "status",
              text: "Статус",
              sortable: true
            },
            {
              field: "name",
              text: "Имя",
              sortable: true
            },
			{
              field: "windows",
              text: "Win",
              sortable: true
            },
            {
              field: "ip",
              text: "IP",
              sortable: true
            },
            {
              field: "local_ip",
              text: "Серый IP",
              sortable: true
            },
            {
              field: "client_id",
              text: "ID Клиента",
              sortable: true
            },
            {
              field: "timestamp",
              text: "От",
              sortable: true
            },
            {
              field: "all",
              text: "Все",
              sortable: false
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
          tableEl.appendChild(thead);

          // Делегирование контекстного меню на tbody (один раз)
          tbodyEl.addEventListener("contextmenu", (event) => {
            const row = event.target.closest("tr[data-id]");
            if (row) showContextMenu(event, row.dataset.id);
          });

          // Делегирование кликов по ячейкам с чекбоксами (один раз)
          tbodyEl.addEventListener("click", (event) => {
            if (event.target.tagName === "INPUT") return;
            const cell = event.target.closest("td");
            if (!cell) return;
            const checkbox = cell.querySelector("input[type='checkbox'][id^='checkbox_']");
            if (!checkbox) return;
            checkbox.checked = !checkbox.checked;
            checkboxStates[checkbox.id] = checkbox.checked;
            sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates));
            updateClientsHeader();
            if (typeof updateClientActionButtons === "function") updateClientActionButtons();
          });

          // Делегирование изменений чекбоксов (один раз)
          tbodyEl.addEventListener("change", (event) => {
            if (event.target.type === "checkbox" && event.target.id.startsWith("checkbox_")) {
              checkboxStates[event.target.id] = event.target.checked;
              sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates));
              updateClientsHeader();
              if (typeof updateClientActionButtons === "function") updateClientActionButtons();
            }
          });

          tableEl.appendChild(tbodyEl);

          // Настройка ячейки "Все" (один раз)
          const allCheckboxCell = thead.querySelector("th[data-field='all']");
          if (allCheckboxCell) {
            allCheckboxCell.addEventListener("click", () => {
              toggleAllCheckboxes();
              if (typeof updateClientActionButtons === "function") updateClientActionButtons();
            });
          }

          clientsContainer.appendChild(tableEl);
        }

        // Определяет текущее направление сортировки из localStorage
        const savedField = localStorage.getItem("sortField") || "name";
        const savedDirStr = localStorage.getItem("sortDirection");
        const isAsc = savedDirStr !== null ? (savedDirStr === "true") : true;
        sortDirections[savedField] = isAsc;

        // Сортирует данные на уровне массива (без обращений к DOM)
        sortClientData(data, savedField, isAsc);

        // Обновляет содержимое tbody, переиспользуя существующие строки где возможно
        renderTbodyRows(tbodyEl, data);

        // Восстанавливает состояния чекбоксов
        restoreCheckboxStates();

        // Обновляет заголовки с подсчётом клиентов
		updateGroupsHeader(!!subgroup);
        updateClientsHeader();

        // Обновляет индикатор сортировки (данные уже отсортированы на уровне массива)
        updateSortIndicator(savedField, isAsc);
      })
      .catch((error) => {
        // Отменённый запрос — не ошибка, просто игнорируется
        if (error.name === "AbortError") return;
        console.error("Ошибка при загрузке данных:", error);
      });
  };
})();
