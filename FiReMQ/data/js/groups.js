// ДИНАМИЧЕСКИЕ ГРУППЫ И ПОДГРУППЫ

document.addEventListener("DOMContentLoaded", function () {
  loadGroups(); // Загрузка групп и подгрупп при загрузке страницы
  fetchAuthName(); // Установка имени авторизованного пользователя (после успешной авторизации) при загрузке страницы
});

// Функция для разворачивания подгрупп
function toggleSubgroups(element) {
  const groupElement = element.closest(".group-item");
  const subgroups = groupElement ? groupElement.querySelector(".subgroups") : null;
  if (subgroups) {
    const isHidden = subgroups.classList.toggle("hidden");
    element.textContent = isHidden ? "▶" : "▼";

    // Имя группы
    const groupName = groupElement.querySelector(".group-name")?.textContent?.trim();
    if (groupName) {
      updateExpandedInSession(groupName, !isHidden);
    }
  }
}

// Сбрасываем выделение выбранной группы/подгруппы
function clearSelectedGroupStyles() {
  document
    .querySelectorAll(".group-item.selected, .subgroup-item.selected")
    .forEach((el) => el.classList.remove("selected"));
}

const ALL_CLIENTS_KEY = "__ALL__";
const SS = {
  selectedGroup: "ui.selectedGroup",
  selectedSubgroup: "ui.selectedSubgroup",
  expandedGroups: "ui.expandedGroups",
};

// Получить Set развернутых групп из sessionStorage
function getExpandedSet() {
  try {
    return new Set(JSON.parse(sessionStorage.getItem(SS.expandedGroups) || "[]"));
  } catch {
    return new Set();
  }
}

// Сохранить Set развернутых групп в sessionStorage
function setExpandedSet(set) {
  sessionStorage.setItem(SS.expandedGroups, JSON.stringify([...set]));
}

// Обновить состояние развернутости конкретной группы
function updateExpandedInSession(groupName, expanded) {
  const s = getExpandedSet();
  if (expanded) s.add(groupName);
  else s.delete(groupName);
  setExpandedSet(s);
}

// Сохранить выбранную группу/подгруппу
function saveSelection(group, subgroup = null) {
  if (group === ALL_CLIENTS_KEY) {
    sessionStorage.setItem(SS.selectedGroup, ALL_CLIENTS_KEY);
    sessionStorage.removeItem(SS.selectedSubgroup);
    return;
  }
  sessionStorage.setItem(SS.selectedGroup, group);
  if (subgroup) sessionStorage.setItem(SS.selectedSubgroup, subgroup);
  else sessionStorage.removeItem(SS.selectedSubgroup);
}

// Принудительно установить группе состояние развернуто/свернуто
function setGroupExpanded(groupElement, expanded) {
  const subgroups = groupElement.querySelector(".subgroups");
  const expandIcon = groupElement.querySelector(".expand-icon");
  if (!subgroups || !expandIcon) return;
  subgroups.classList.toggle("hidden", !expanded);
  expandIcon.textContent = expanded ? "▼" : "▶";
}

// Найти DOM-элемент группы по имени
function findGroupElementByName(name) {
  const items = document.querySelectorAll("#groupsContainer .group-item");
  for (const el of items) {
    const label = el.querySelector(".group-name")?.textContent?.trim();
    if (label === name) return el;
  }
  return null;
}

// Восстановить развороты и выбранную группу/подгруппу после рендера списка
function restoreGroupsState() {
  // 1) Развернуть сохраненные группы
  const expandedArr = JSON.parse(sessionStorage.getItem(SS.expandedGroups) || "[]");
  expandedArr.forEach((groupName) => {
    const el = findGroupElementByName(groupName);
    if (el) setGroupExpanded(el, true);
  });

  // 2) Восстановить выбранную группу/подгруппу и загрузить клиентов
  const selGroup = sessionStorage.getItem(SS.selectedGroup);
  const selSubgroup = sessionStorage.getItem(SS.selectedSubgroup);

  if (selGroup === ALL_CLIENTS_KEY) {
    const all = document.getElementById("all-clients-group");
    if (all) {
      clearSelectedGroupStyles();
      all.classList.add("selected");
    }
    loadClients();
    return;
  }

  if (selGroup) {
    const groupEl = findGroupElementByName(selGroup);
    if (groupEl) {
      clearSelectedGroupStyles();
      groupEl.classList.add("selected");

      if (selSubgroup) {
        setGroupExpanded(groupEl, true); // раскрыть родителя
        const subEls = groupEl.querySelectorAll(".subgroup-item");
        for (const subEl of subEls) {
          if (subEl.textContent.trim() === selSubgroup) {
            subEl.classList.add("selected");
            break;
          }
        }
        loadClients(selGroup, selSubgroup);
      } else {
        loadClients(selGroup);
      }
    } else {
      // Группа не найдена (удалили/переименовали) — откроем "Все клиенты"
      const all = document.getElementById("all-clients-group");
      if (all) {
        clearSelectedGroupStyles();
        all.classList.add("selected");
      }
      loadClients();
    }
  }
}

// Функция загрузки групп и подгрупп с сервера
function loadGroups() {
  fetch("/get-all-groups-and-sub-groups")
    .then((response) => {
      if (!response.ok) {
        throw new Error(`Ошибка: ${response.status}`);
      }
      return response.json();
    })
    .then((data) => {
      const groupsContainer = document.getElementById("groupsContainer");
      groupsContainer.innerHTML = ""; // Очищаем текущее содержимое контейнера групп

      // Отделяем "Новые клиенты" и сортируем группы
      const groups = Object.keys(data);
      const subgroupsMap = data;
      const newClientsGroup = groups.includes("Новые клиенты") ? "Новые клиенты" : null;

      // Убираем "Новые клиенты" из списка для сортировки
      if (newClientsGroup) {
        groups.splice(groups.indexOf(newClientsGroup), 1);
      }

      // Сортируем оставшиеся группы
      const sortedGroups = groups.sort((a, b) => a.localeCompare(b, "ru"));

      // Добавляем "Новые клиенты" на второе место
      if (newClientsGroup) {
        sortedGroups.unshift(newClientsGroup); // Вставляем в начало отсортированного списка
      }

      // Создаём элементы для каждой группы
      sortedGroups.forEach((group) => {
        const groupElement = createGroupElement(group, subgroupsMap[group]);
        groupsContainer.appendChild(groupElement);
      });
	  
	  restoreGroupsState(); // Восстанавливаем развороты и выбор после рендера
	  
    })
    .catch((error) => {
      console.error("Ошибка при загрузке групп:", error);
    });
}

// Создание HTML-элемента для группы
function createGroupElement(group, subgroups) {
  const groupElement = document.createElement("div");
  groupElement.className = "group-item";

groupElement.addEventListener("click", () => {
  hideContextMenu(); // Закрывает меню

  // Сохраняем выбор
  saveSelection(group, null);

  loadClients(group);

  // Выделяем выбранную группу
  clearSelectedGroupStyles();
  groupElement.classList.add("selected");
});

  // Создаем стрелку для разворачивания
  const expandIcon = document.createElement("span");
  expandIcon.className = "expand-icon";
  expandIcon.textContent = "▶";
  expandIcon.onclick = (e) => {
    e.stopPropagation(); // Останавливаем всплытие события клика
    toggleSubgroups(expandIcon);
  };

  // Добавляем название группы
  const groupName = document.createElement("span");
  groupName.className = "group-name";
  groupName.textContent = group;

  // Контейнер для подгрупп
  const subgroupsContainer = document.createElement("div");
  subgroupsContainer.className = "subgroups hidden";

  // Сортируем подгруппы перед созданием элементов
  const sortedSubgroups = subgroups.sort((a, b) => a.localeCompare(b, "ru"));

  // Создаем подгруппы
  sortedSubgroups.forEach((subgroup) => {
    const subgroupElement = document.createElement("div");
    subgroupElement.className = "subgroup-item";
    subgroupElement.textContent = subgroup; // Название подгруппы
	
	subgroupElement.addEventListener("click", (e) => {
	  e.stopPropagation();// Останавливает всплытие события клика
	  hideContextMenu(); // Закрывает меню

	  // Сохраняем выбор
	  saveSelection(group, subgroup);

	  loadClients(group, subgroup);

	  // Выделяем и подгруппу, и её родительскую группу
	  clearSelectedGroupStyles();
	  subgroupElement.classList.add("selected");
	  groupElement.classList.add("selected");
	});
		
    subgroupsContainer.appendChild(subgroupElement);
  });

  // Собираем элементы группы
  groupElement.appendChild(expandIcon);
  groupElement.appendChild(groupName);
  if (sortedSubgroups.length > 0) {
    groupElement.appendChild(subgroupsContainer);
  }

  return groupElement;
}

// Для "Все клиенты"
document.addEventListener("DOMContentLoaded", function() {
  const allClientsGroup = document.getElementById("all-clients-group");
  if (allClientsGroup) {
    allClientsGroup.addEventListener("click", function() {
	  hideContextMenu(); // Закрывает меню

	  // Сохраняем выбор "Все клиенты"
	  saveSelection(ALL_CLIENTS_KEY);

	  loadClients(); // Вызов функции без параметров

	  // Выделяем "Все клиенты"
	  clearSelectedGroupStyles();
	  allClientsGroup.classList.add("selected");
	});
  }
});