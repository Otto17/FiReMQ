// ОБЩИЕ ФУНКЦИИ ДЛЯ МОДАЛЬНЫХ ОКОН

// Глобальный обработчик нажатия клавиш (для закрытия окна кнопкой "Esc")
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") {
	// Проверяем окно "Список полезных команд"
	const cheatsheetModal = document.getElementById("commandCheatsheetModal");
	if (cheatsheetModal && cheatsheetModal.style.display === "flex") {
	  cheatsheetModal.style.display = "none";
	  event.stopPropagation();
	  event.preventDefault();
	  return; // Закрыли только "Список полезных команд"
	}
	
    // Проверяем окно подтверждения отмены загрузки
    const confirmCancelUploadModal = document.getElementById("confirmCancelUploadModal");
    if (confirmCancelUploadModal && confirmCancelUploadModal.style.display === "flex") {
      confirmCancelUploadModal.style.display = "none";
      event.stopPropagation();
      event.preventDefault();
      return;
    }

    // Проверяем другие окна подтверждения
    const confirmModal = document.getElementById("confirmDeleteRequestModal");
    if (confirmModal && confirmModal.style.display === "flex") {
      // Если окно подтверждения на переднем плане, закрываем его и не закрываем окно отчётов
      confirmModal.style.display = "none";
      event.stopPropagation();
      event.preventDefault();
    } else {
      // Если окно подтверждения не активно, закрываем другие окна
      closeDeleteModal(); 				// Для одиночного удаления
      closeDeleteCheckModal(); 			// Для массового удаления
      closeMoveModal(); 				// Для перемещения клиента в группу
      closeMoveCheckModal(); 			// Для массового перемещения клиентов в группу
      closeAccountsModal(); 			// Для учётных записей Админов
      closeMqttAuthModal(); 			// Для MQTT авторизации
      closeExecuteCommandModal();		// Для выполнения CMD или PowerShell команды
      closeInstallProgramModal();		// Для установки ПО
      closeReportModal(); 				// Для отчётов По установкам и cmd / PowerShell
	  closeAboutModal(); 				// Для окна "О программе"
	  closeUninstallFiReAgentModal();	// Для удаления FiReAgent
    }
  }
});

// Проверка "sessionStorage" (Хранение сеансов) после перезагрузки страницы для вывода push-уведомлений
window.addEventListener("load", function() {
  const message = sessionStorage.getItem("pushMessage");
  const color = sessionStorage.getItem("pushColor");
  if (message && color) {
    showPush(message, color);
	
    // Очищаем "sessionStorage" после показа уведомления
    sessionStorage.removeItem("pushMessage");
    sessionStorage.removeItem("pushColor");
  }
});

// Функция для валидации ввода (без знака пробела, либо с ним и доп. символами)
function validateInput(input, allowSpace = false) {
  const regex = allowSpace ? /^[a-zA-Z0-9а-яА-ЯёЁ _!@#$%.\/?\-\*+=,:|()"'@–—]+$/ : /^[a-zA-Z0-9а-яА-ЯёЁ_!@#$%.\/?-]+$/;
  return regex.test(input);
}



// МОДАЛЬНОЕ ОКНО УДЛАЛЕНИЯ КЛИЕНТА

let currentClientName = null; // Имя текущего клиента для удаления

// Удаление клиента
function deleteClient() {
  const clientRow = document.querySelector(`tr[data-id="${currentClientID}"]`);
  const clientName = clientRow ? clientRow.querySelector('[data-field="name"]').textContent : "неизвестный";

  showDeleteModal(clientName); // Отображаем модальное окно с именем клиента
}

// Показ модального окна удаления клиента
function showDeleteModal(clientName) {
  const modal = document.getElementById("deleteClientModal");
  const clientNameToDelete = document.getElementById("clientNameToDelete");
  const confirmDeleteButton = document.getElementById("confirmDeleteButton");

  currentClientName = clientName; // Сохраняем имя текущего клиента
  clientNameToDelete.textContent = clientName; // Устанавливаем имя клиента в модальное окно

  modal.style.display = "flex"; // Показываем модальное окно

  // Устанавливаем фокус на кнопку "Удалить"
  confirmDeleteButton.focus();
}

// Закрытие модального окна "Удалить"
function closeDeleteModal() {
  const modal = document.getElementById("deleteClientModal");
  modal.style.display = "none"; // Скрываем модальное окно
}

// Подтверждение удаления клиента
function confirmDeleteClient() {
	const requestData = { clientID: currentClientID }; // Сборка данных для POST
	apiPostJson("/delete-client", requestData)

    .then((response) => {
        if (!response.ok) {
            return response.text().then((errorText) => {
                throw new Error(errorText);
            });
        }
        return response.text();
    })
    .then((data) => {
        sessionStorage.setItem("pushMessage", data);
        sessionStorage.setItem("pushColor", "#2196F3"); // Голубой
        location.reload(); // Перезагружаем страницу
    })
    .catch((error) => {
        showPush(error.message, "#ff4d4d"); // Красный
    });
}

// Привязка событий к элементам модального окна
document.getElementById("closeDeleteModal").addEventListener("click", closeDeleteModal);
document.getElementById("confirmDeleteButton").addEventListener("click", confirmDeleteClient);



// МОДАЛЬНОЕ ОКНО МАССОВОГО УДАЛЕНИЯ КЛИЕНТОВ

// Удаление выделенных клиентов
function deleteCheckClient() {
  const checkedClients = Object.keys(checkboxStates).filter((clientId) => checkboxStates[clientId] === true);

  if (checkedClients.length === 0) {
	showPush("Нет выбранных клиентов для удаления.", "#ff4d4d"); // Красный
    return;
  }

  // Показ нового модального окна
  showDeleteCheckClientsModal();
}

// Показ модального окна для удаления выделенных клиентов
function showDeleteCheckClientsModal() {
  const modal = document.getElementById("deleteCheckClientsModal");
  const deleteConfirmationInput = document.getElementById("deleteConfirmationInput");
  const confirmDeleteCheckButton = document.getElementById("confirmDeleteCheckButton");

  // Сброс состояния поля ввода и кнопки
  deleteConfirmationInput.value = "";
  confirmDeleteCheckButton.disabled = true;

  modal.style.display = "flex"; // Показываем модальное окно
}

// Закрытие модального окна "Удалить выделенных"
function closeDeleteCheckModal() {
  const modal = document.getElementById("deleteCheckClientsModal");
  modal.style.display = "none"; // Скрываем модальное окно
}

// Обработчик ввода текста для активации кнопки удаления (регистронезависимый ввод)
document.getElementById("deleteConfirmationInput").addEventListener("input", function(event) {
  const confirmDeleteCheckButton = document.getElementById("confirmDeleteCheckButton");
  const inputValue = event.target.value.trim().toLowerCase(); // Приводим ввод к нижнему регистру
  confirmDeleteCheckButton.disabled = inputValue !== "удаляем!";
});

// Подтверждение удаления выделенных клиентов
function confirmDeleteCheckClients() {
  const checkedClients = Object.keys(checkboxStates)
    .filter((clientId) => checkboxStates[clientId] === true)
    .map((clientId) => clientId.replace("checkbox_", "")); // Убираем префикс "checkbox_"

  if (checkedClients.length === 0) {
	showPush("Нет выбранных клиентов для удаления.", "#ff4d4d"); // Красный
    return;
  }

	apiPostJson("/delete-selected-clients", checkedClients)
	
    .then((response) => {
      if (!response.ok) {
        throw new Error(`HTTP error! Status: ${response.status}`);
      }
      return response.text();
    })
    .then((data) => {
      // Удаляем удалённых клиентов из sessionStorage
      checkedClients.forEach((clientId) => {
        const checkboxKey = `checkbox_${clientId}`; // Восстанавливаем ключ с префиксом
        delete checkboxStates[checkboxKey];
        sessionStorage.removeItem(checkboxKey);
      });

      // Обновляем sessionStorage с изменённым состоянием
      sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates));

      // Сохраняем сообщение и цвет push-уведомления в "sessionStorage" (Хранение сеансов)
      sessionStorage.setItem("pushMessage", data);
	  sessionStorage.setItem("pushColor", "#2196F3"); // Голубой
      location.reload(); // Перезагрузка страницы
    })
    .catch((error) => {
      console.error("Ошибка удаления выделенных клиентов:", error);
    });

  closeDeleteCheckModal(); // Закрыть модальное окно
}

// Привязка событий к новому модальному окну
document.getElementById("closeDeleteCheckModal").addEventListener("click", closeDeleteCheckModal);
document.getElementById("confirmDeleteCheckButton").addEventListener("click", confirmDeleteCheckClients);

// Модальное окно массового удаления клиентов
document.getElementById("deleteCheckClientsModal").addEventListener("keydown", function(event) {
  if (event.key === "Enter") {
    const input = document.getElementById("deleteConfirmationInput");
    if (input.value.trim().toLowerCase() === "удаляем!") {
      document.getElementById("confirmDeleteCheckButton").click();
    }
  }
});



// МОДАЛЬНОЕ ОКНО ПЕРЕМЕЩЕНИЯ КЛИЕНТА В ДРУГУЮ ПОДГРУППУ

document.addEventListener("DOMContentLoaded", function() {
  const moveClientModal = document.getElementById("moveClientModal");
  const closeMoveClientModal = document.getElementById("closeMoveClientModal");
  const confirmMoveClientButton = document.getElementById("confirmMoveClientButton");

  //Закрытие модального окна при нажатии на крестик
  closeMoveClientModal.onclick = () => (moveClientModal.style.display = "none");

  // Открытие модального окна перемещения клиента
  const moveClientBtn = document.getElementById("moveClient");
  if (moveClientBtn) {
	moveClientBtn.addEventListener("click", () => {
		loadExistingGroups(); // Автоматическая загрузка групп и подгрупп
		document.getElementById("moveClientModal").style.display = "flex";
	});
  }

  // Обработка выбора радиокнопок
  document.querySelectorAll('input[name="moveOption"]').forEach((radio) =>
    radio.addEventListener("change", (e) => {
      const isExisting = e.target.value === "existing";
      document.getElementById("existingGroups").disabled = !isExisting;
      document.getElementById("newGroupName").disabled = isExisting;
      document.getElementById("newSubgroupName").disabled = isExisting;
    })
  );

  // Перемещение клиента
	confirmMoveClientButton.onclick = () => {
		const clientID = currentClientID;
		const moveOption = document.querySelector('input[name="moveOption"]:checked').value;

		let newGroupID, newSubgroupID;
		if (moveOption === "existing") {
			const selectedOption = document.getElementById("existingGroups").value;
			if (!selectedOption) {
				showPush("Выберите существующую группу и подгруппу.", "#ff4d4d"); // Красный
				return;
			}
			[newGroupID, newSubgroupID] = selectedOption.split("|");
		} else {
			newGroupID = document.getElementById("newGroupName").value.trim();
			newSubgroupID = document.getElementById("newSubgroupName").value.trim();
			if (!newGroupID || !newSubgroupID) {
				showPush("Введите названия новой группы и подгруппы.", "#ff4d4d"); // Красный
				return;
			}
		}

			// Формирование данных для POST
			const requestData = {
				clientID: clientID,
				newGroupID: newGroupID,
				newSubgroupID: newSubgroupID,
			};

			apiPostJson("/move-client", requestData)
			
			.then((response) => {
				if (!response.ok) {
					return response.text().then((errorText) => {
						throw new Error(errorText);
					});
				}
				return response.text();
			})
			.then((data) => {
				moveClientModal.style.display = "none";

				// Сохраняем сообщение и цвет push-уведомления в "sessionStorage" (Хранение сеансов)
				sessionStorage.setItem("pushMessage", data);
				sessionStorage.setItem("pushColor", "#4CAF50"); // Зелёный
				location.reload(); // Перезагружаем страницу после успешного перемещения
			})
			.catch((error) => {
				showPush(error.message, "#ff4d4d"); // Красный
			});
	};
});

// Подгрузка существующих групп и подгрупп
function loadExistingGroups() {
  fetch("/get-all-groups-and-sub-groups")
    .then((response) => response.json())
    .then((data) => {
      const existingGroupsSelect = document.getElementById("existingGroups");
      existingGroupsSelect.innerHTML = `<option value="" disabled selected>Выберите группу и подгруппу</option>`;

      let currentGroup = null; // Текущая группа
      for (const [group, subgroups] of Object.entries(data)) {
        if (group !== currentGroup) {
          // Добавляем разделитель перед началом новой группы
          const divider = document.createElement("option");
          divider.disabled = true;
          divider.textContent = "---";
          existingGroupsSelect.appendChild(divider);

          currentGroup = group;
        }

        subgroups.forEach((subgroup) => {
          const option = document.createElement("option");
          option.value = `${group}|${subgroup}`;
          option.textContent = `${group} / ${subgroup}`;
          existingGroupsSelect.appendChild(option);
        });
      }
    })
    .catch((error) => console.error("Ошибка загрузки групп:", error));
}

// Отображение выбранной группы и подгруппы
document.getElementById("existingGroups").addEventListener("change", function() {
  const selectedOption = this.value;

  if (selectedOption) {
    const [group, subgroup] = selectedOption.split("|");
    this.options[0].textContent = `Выбрана: ${group} / ${subgroup}`; // Меняем текст в первой строке
    this.options[0].classList.add("highlight-selected"); // Добавляем класс выделения
  }
});

// Поддержка нажатия Enter для перемещения клиента
document.getElementById("moveClientForm").addEventListener("keydown", function(event) {
  if (event.key === "Enter") {
    document.getElementById("confirmMoveClientButton").click();
  }
});

// Закрытие модального окна "Переместить в..."
function closeMoveModal() {
  const modal = document.getElementById("moveClientModal");
  modal.style.display = "none"; // Скрываем модальное окно
}



// МОДАЛЬНОЕ ОКНО ДЛЯ МАССОВОГО ПЕРЕМЕЩЕНИЯ КЛИЕНТОВ В ДРУГУЮ ПОДГРУППУ

// Открытие модального окна массового перемещения клиентов
function moveClientCheckClient() {
  const checkedClients = Object.keys(checkboxStates).filter((clientId) => checkboxStates[clientId] === true);

  if (checkedClients.length === 0) {
	showPush("Нет выбранных клиентов для перемещения.", "#ff4d4d"); // Красный
    return;
  }

  loadExistingGroupsCheck(); // Загрузка списка групп и подгрупп
  showMoveCheckClientsModal(); // Показываем модальное окно
}

// Показ модального окна перемещения выделенных клиентов
function showMoveCheckClientsModal() {
  const modal = document.getElementById("moveCheckClientsModal");
  modal.style.display = "flex";
}

// Закрытие модального окна
function closeMoveCheckModal() {
  const modal = document.getElementById("moveCheckClientsModal");
  modal.style.display = "none";
}

// Привязка обработчиков событий к модальному окну
document.getElementById("closeMoveCheckModal").addEventListener("click", closeMoveCheckModal);

// Обработка выбора опций (существующая группа/новая)
document.querySelectorAll('#moveCheckClientsForm input[name="moveOption"]').forEach((radio) =>
  radio.addEventListener("change", (e) => {
    const isExisting = e.target.value === "existing";
    document.getElementById("existingGroupsCheck").disabled = !isExisting;
    document.getElementById("newGroupNameCheck").disabled = isExisting;
    document.getElementById("newSubgroupNameCheck").disabled = isExisting;
  })
);

// Подтверждение массового перемещения клиентов
document.getElementById("confirmMoveCheckClientsButton").addEventListener("click", () => {
  const checkedClients = Object.keys(checkboxStates)
    .filter((clientId) => checkboxStates[clientId] === true)
    .map((clientId) => clientId.replace("checkbox_", "")); // Убираем префикс "checkbox_"

  if (checkedClients.length === 0) {
    showPush("Нет выбранных клиентов для перемещения.", "#ff4d4d"); // Красный
    return;
  }

  const moveOption = document.querySelector('#moveCheckClientsForm input[name="moveOption"]:checked').value;
  let newGroupID, newSubgroupID;

  if (moveOption === "existing") {
    const selectedOption = document.getElementById("existingGroupsCheck").value;
    if (!selectedOption) {
      showPush("Выберите существующую группу и подгруппу.", "#ff4d4d"); // Красный
      return;
    }
    [newGroupID, newSubgroupID] = selectedOption.split("|");
  } else {
    newGroupID = document.getElementById("newGroupNameCheck").value.trim();
    newSubgroupID = document.getElementById("newSubgroupNameCheck").value.trim();
    if (!newGroupID || !newSubgroupID) {
      showPush("Введите названия новой группы и подгруппы.", "#ff4d4d"); // Красный
      return;
    }
  }

	// Формирование данных для POST
	const requestData = {
		clientIDs: checkedClients,
        newGroup: newGroupID,
        newSubgroup: newSubgroupID,
	};

	apiPostJson("/move-selected-clients", requestData)
     .then((response) => {
        if (!response.ok) {
            // Если сервер вернул ошибку (статус не 2xx), пытаемся извлечь сообщение или используем стандартное, и прерываем цепочку
            return response.json().then(errData => {
                throw new Error(errData.message || "Ошибка перемещения клиентов");
            }).catch(() => {
                throw new Error("Ошибка перемещения клиентов");
            });
        }
        // Если ответ успешный, парсим его как JSON
        return response.json();
    })
    .then((data) => {
        clearAllCheckboxStates(); // Снимаем все галочки в sessionStorage

        let pushMessage = data.message;
        let pushColor;

        // Определяем цвет на основе поля "status"
        if (data.status === "Предупреждение") {
            pushColor = "#ff4081"; // Розовый
        } else { // "Успех" или любой другой положительный статус
            pushColor = "#4CAF50"; // Зелёный
        }

        // Сохраняем сообщение и цвет push-уведомления в sessionStorage
        sessionStorage.setItem("pushMessage", pushMessage);
        sessionStorage.setItem("pushColor", pushColor);
        
        location.reload(); // Перезагружаем страницу для отображения изменений
    })
	// Критические ошибоки
    .catch((error) => showPush(error.message, "#ff4d4d")); // Красный

  closeMoveCheckModal(); // Закрываем модальное окно
});

// Очистка (false) выделенных клиентов в "sessionStorage" после массового премещения клиентов
function clearAllCheckboxStates() {
  for (const clientId in checkboxStates) {
    if (checkboxStates.hasOwnProperty(clientId)) {
      checkboxStates[clientId] = false; // Устанавливаем состояние в false
    }
  }
  // Сохраняем изменения в sessionStorage
  sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates));
}

// Загрузка существующих групп и подгрупп в модальном окне
function loadExistingGroupsCheck() {
  fetch("/get-all-groups-and-sub-groups")
    .then((response) => response.json())
    .then((data) => {
      const existingGroupsSelect = document.getElementById("existingGroupsCheck");
      existingGroupsSelect.innerHTML = `<option value="" disabled selected>Выберите группу и подгруппу</option>`;

      let currentGroup = null; // Текущая группа
      for (const [group, subgroups] of Object.entries(data)) {
        if (group !== currentGroup) {
          const divider = document.createElement("option");
          divider.disabled = true;
          divider.textContent = "---";
          existingGroupsSelect.appendChild(divider);
          currentGroup = group;
        }

        subgroups.forEach((subgroup) => {
          const option = document.createElement("option");
          option.value = `${group}|${subgroup}`;
          option.textContent = `${group} / ${subgroup}`;
          existingGroupsSelect.appendChild(option);
        });
      }
    })
    .catch((error) => console.error("Ошибка загрузки групп:", error));
}

// Отображение выбранной группы и подгруппы
document.getElementById("existingGroupsCheck").addEventListener("change", function() {
  const selectedOption = this.value;

  if (selectedOption) {
    const [group, subgroup] = selectedOption.split("|");
    this.options[0].textContent = `Выбрана: ${group} / ${subgroup}`; // Меняем текст в первой строке
    this.options[0].classList.add("highlight-selected"); // Добавляем класс выделения
  }
});

// Поддержка нажатия Enter для массового перемещения клиентов
document.getElementById("moveCheckClientsForm").addEventListener("keydown", function(event) {
  if (event.key === "Enter") {
    document.getElementById("confirmMoveCheckClientsButton").click();
  }
});



// МОДАЛЬНОЕ ОКНО УЧЁТНЫЕ ЗАПИСИ АДМИНОВ

// Открытие модального окна
function openAccountsModal() {
  document.getElementById("accountsModal").style.display = "flex";
  loadAccounts();
  
	// Обработчики для кнопок переключения пароля
    document.querySelectorAll('.toggle-password').forEach(button => {
        button.addEventListener('click', () => {
            const targetId = button.getAttribute('data-target');
            togglePasswordVisibility(targetId);
        });
    });
}

// Закрытие модального окна
function closeAccountsModal() {
  document.getElementById("accountsModal").style.display = "none";
}

// Загрузка учётных записей
function loadAccounts() {
    fetch("/get-admin-names", {
    })
        .then((response) => response.json())
        .then((data) => {
            const accountsList = document.getElementById("accountsList");
            accountsList.innerHTML = "";
            data.forEach((user) => {
                // Создаём форму для каждой учётной записи
                const accountForm = document.createElement("form");
                accountForm.className = "account-form";

                // Создаём контейнер для элементов учётной записи
                const accountItem = document.createElement("div");
                accountItem.className = "account-item";
                const encodedLogin = encodeURIComponent(user.auth_login);

                // Поле ввода имени
                const nameInput = document.createElement("input");
                nameInput.type = "text";
                nameInput.name = `update-name-${encodedLogin}`;
                nameInput.placeholder = "Обновить имя (до 40)";
                nameInput.maxLength = 40;
                nameInput.value = user.auth_name;
				nameInput.setAttribute("origNameAdmin", user.auth_name);
                nameInput.dataset.login = encodedLogin;
                nameInput.className = "update-name";
                nameInput.required = true;

                // Отображение логина
                const loginDisplay = document.createElement("div");
                loginDisplay.className = "login-display";
                const loginSpan = document.createElement("span");
                loginSpan.className = "login-name";
                loginSpan.textContent = user.auth_login;
                loginDisplay.appendChild(loginSpan);

                // Контейнер для пароля
                const passwordContainer = document.createElement("div");
                passwordContainer.className = "password-input-container";
                const passwordInput = document.createElement("input");
                passwordInput.type = "password";
                passwordInput.name = `update-password-${encodedLogin}`;
                passwordInput.placeholder = "Обновить пароль (до 64)";
                passwordInput.maxLength = 64;
                passwordInput.dataset.login = encodedLogin;
                passwordInput.className = "update-password";
                passwordInput.autocomplete = "off";

                // Кнопка переключения видимости пароля
                const toggleButton = document.createElement("button");
                toggleButton.type = "button";
                toggleButton.className = "toggle-password";
                toggleButton.dataset.login = encodedLogin;
                const toggleIcon = document.createElement("img");
                toggleIcon.id = `passwordIcon-${encodedLogin}`;
                toggleIcon.src = "../icon/Hide_Passwd.svg";
                toggleIcon.alt = "Показать пароль";
                toggleButton.appendChild(toggleIcon);
                passwordContainer.appendChild(passwordInput);
                passwordContainer.appendChild(toggleButton);

                // Информация о датах
                const dateInfo = document.createElement("div");
                dateInfo.className = "date-info";
                if (user.date_change === "--.--.--(--:--)")
                    dateInfo.classList.add("padding-right-26");
                dateInfo.innerHTML = `Создан: ${user.date_create} | Изменён: ${user.date_change}`;

                // Кнопка "Обновить"
                const updateButton = document.createElement("button");
                updateButton.type = "button"; // Изменяем тип на button
                updateButton.className = "save-buttonModal update-button";
                updateButton.dataset.login = encodedLogin;
                updateButton.textContent = "Обновить";

                // Кнопка "Удалить"
                const deleteButton = document.createElement("button");
                deleteButton.type = "button";
                deleteButton.className = "save-buttonModal delete-button";
                deleteButton.dataset.login = encodedLogin;
                deleteButton.textContent = "Удалить";

                // Собираем элементы в "accountItem"
                accountItem.appendChild(nameInput);
                accountItem.appendChild(loginDisplay);
                accountItem.appendChild(passwordContainer);
                accountItem.appendChild(dateInfo);
                accountItem.appendChild(updateButton);
                accountItem.appendChild(deleteButton);

                // Добавляем "accountItem" в форму
                accountForm.appendChild(accountItem);

                // Добавляем форму в "accountsList"
                accountsList.appendChild(accountForm);

                // Обработчики событий для кнопок
                toggleButton.addEventListener("click", () => {
                    togglePasswordVisibility(encodedLogin);
                });
                updateButton.addEventListener("click", (event) => {
                    event.preventDefault(); // Предотвращаем любые действия формы
                    toggleUpdateButtonState(encodedLogin);
                });
                deleteButton.addEventListener("click", () => {
                    toggleDeleteButtonState(encodedLogin);
                });
            });
        })
        .catch((error) => console.error("Ошибка загрузки учётных записей:", error));
}

// Обновление учётной записи
function updateUser(login) {
    const newName = document.querySelector(`.update-name[data-login="${login}"]`).value;
    const newPassword = document.querySelector(`.update-password[data-login="${login}"]`).value;

    const updateData = {
        auth_login: login,
        auth_new_name: newName,
        auth_new_password: newPassword,
    };

	apiPostJson("/update-admin", updateData)	
	
    .then((response) => response.text())
    .then((message) => {
        showPush(message, "#2196F3");
        loadAccounts();
		
		if (newName.trim() !== '') {
                fetchAuthName();
        }
    })
    .catch((error) => console.error("Ошибка обновления пользователя:", error));
}

// Функция для изменения состояния кнопки "Обновить"
function toggleUpdateButtonState(login) {
    const updateButton = document.querySelector(`button.update-button[data-login="${login}"]`);

    if (!updateButton) return;

    const handleConfirm = (e) => {
        e.preventDefault();
        updateUser(login);
    };

    const handleCancel = (e) => {
        if (e.button === 0) {
            resetUpdateButtonState(updateButton);
        }
    };

    const resetUpdateButtonState = (button) => {
        button.textContent = "Обновить";
        button.classList.remove("confirm-mode");
        button.removeEventListener("contextmenu", handleConfirm);
        button.removeEventListener("click", handleCancel);
    };

    if (!updateButton.classList.contains("confirm-mode")) {
		const nameInput = document.querySelector(`.update-name[data-login="${login}"]`);
		const origName = nameInput.getAttribute("origNameAdmin").trim();
        const newName = nameInput.value.trim();
        const newPassword = document.querySelector(`.update-password[data-login="${login}"]`).value;

        // Проверка на пустое имя
        if (!newName) {
            showPush("Имя пользователя должно быть заполнено.", "#ff4081"); // Розовый
            return;
        }

		// Проверка на совпадение имени и пустой пароль
        if (newName === origName && newPassword === "") {
            showPush("Имя админа совпадает с текущим!", "#ff4081"); // Розовый
            return;
        }
		
		// Проверка на спецсимволы по каждому полю отдельно
		if (!validateInput(newName, true)) {
			showPush("Имя обновляемого админа содержит запрещённые символы!", "#ff4081"); // Розовый
			return;
		}
		
		if (newPassword && !validateInput(newPassword)) {
            showPush("Пароль обновляемого админа содержит запрещённые символы!", "#ff4081"); // Розовый
            return;
        }

        updateButton.textContent = "Подтверди ПКМ";
        updateButton.classList.add("confirm-mode");

        updateButton.addEventListener("contextmenu", handleConfirm);
        updateButton.addEventListener("click", handleCancel);
    }
}

// Удаление учётной записи
function deleteUser(login) {
    const deleteData = { auth_login: login };
	apiPostJson("/delete-admin", deleteData)
 
    .then(response => {
        const status = response.status;
        return response.text().then(text => ({ status, text }));
    })
    .then(({ status, text }) => {
        if (status === 200) {
            showPush(text, "#ff4d4d"); // Красный
            loadAccounts();
        } else if (status === 401) {
            showPush(text, "#ff4d4d"); // Красный
            // Перезагружаем страницу через 1,5 секунды (что бы успеть увидеть PUSH уведомление)
            setTimeout(() => {
                window.location.href = "/auth.html";
            }, 1500);
        } else {
            showPush(text, "#ff4d4d"); // Красный
        }
    })
    .catch((error) => console.error("Ошибка удаления пользователя:", error));
}

// Функция для изменения состояния кнопки "Удалить"
function toggleDeleteButtonState(login) {
  // Используем более надёжный селектор
  const deleteButton = document.querySelector(`button.save-buttonModal[data-login="${login}"]:not(.update-button)`);

  // Проверяем, что кнопка найдена
  if (!deleteButton) {
    console.error('Кнопка "Удалить" не найдена для логина:', login);
    return;
  }

  const handleConfirm = (e) => {
    e.preventDefault();
    deleteUser(login);
  };

  const handleCancel = (e) => {
    if (e.button === 0) {
      resetDeleteButtonState(deleteButton);
    }
  };

  const resetDeleteButtonState = (button) => {
    button.textContent = "Удалить";
    button.classList.remove("confirm-mode");

    button.removeEventListener("contextmenu", handleConfirm);
    button.removeEventListener("click", handleCancel);
  };

  if (!deleteButton.classList.contains("confirm-mode")) {
    deleteButton.textContent = "Подтверди ПКМ";
    deleteButton.classList.add("confirm-mode");

    deleteButton.removeEventListener("contextmenu", handleConfirm);
    deleteButton.removeEventListener("click", handleCancel);

    deleteButton.addEventListener("contextmenu", handleConfirm);
    deleteButton.addEventListener("click", handleCancel);
  }
}

// Добавление новой учётной записи
document.getElementById("addUserForm").addEventListener("submit", function(event) {
  event.preventDefault();

  const newName = document.getElementById("newName").value;
  const newLogin = document.getElementById("newLogin").value;
  const newPassword = document.getElementById("newPassword").value;

  // Проверка на пустые поля
  if (!newName || !newLogin || !newPassword) {
    showPush("Все поля должны быть заполнены.", "#ff4081"); // Розовый
    return;
  }

  // Проверка на спецсимволы по каждому полю отдельно
    if (!validateInput(newName, true)) {
        showPush("Имя нового админа содержит запрещённые символы!", "#ff4081"); // Розовый
        return;
    }
	
    if (!validateInput(newLogin)) {
        showPush("Логин нового админа содержит запрещённые символы!", "#ff4081"); // Розовый
        return;
    }
	
    if (!validateInput(newPassword)) {
        showPush("Пароль нового админа содержит запрещённые символы!", "#ff4081"); // Розовый
        return;
    }

  const addData = {
    auth_name: newName,
    auth_login: newLogin,
    auth_password: newPassword,
  };

  apiPostJson("/add-admin", addData)
  
    .then(response => {
        if (response.ok) {
            return response.text();
        } else if (response.status === 409) {
            return response.text().then(text => Promise.reject(text));
        } else {
            return response.text().then(text => Promise.reject("Ошибка сервера: " + text));
        }
    })
    .then(message => {
        showPush(message, "#4CAF50"); // Зелёный
        loadAccounts();
        document.getElementById("addUserForm").reset();
    })
    .catch(errorMessage => {
        showPush(errorMessage, "#ff4081"); // Розовый
    });
});

// Обработка клика по ссылке "Учётные записи"
document.getElementById("accountsLink").addEventListener("click", function(event) {
  event.preventDefault(); // Предотвращаем стандартное поведение ссылки
  openAccountsModal();
});

// Функция для переключения видимости пароля
function togglePasswordVisibility(elementId) {
  let passwordInput, passwordIcon;

  if (elementId.startsWith("new")) {
    passwordInput = document.getElementById(elementId);
    passwordIcon = document.getElementById(`${elementId}Icon`);
  } else {
    passwordInput = document.querySelector(`.update-password[data-login="${elementId}"]`);
    passwordIcon = document.getElementById(`passwordIcon-${elementId}`);
  }

  if (passwordInput && passwordIcon) {
    const isPasswordHidden = passwordInput.getAttribute("type") === "password";
    passwordInput.setAttribute("type", isPasswordHidden ? "text" : "password");
    passwordIcon.setAttribute("src", isPasswordHidden ? "../icon/Show_Passwd.svg" : "../icon/Hide_Passwd.svg");
    passwordIcon.setAttribute("alt", isPasswordHidden ? "Показать пароль" : "Скрыть пароль");
  }
}

// Обработчик закрытия модального окна крестиком
document.addEventListener("DOMContentLoaded", function() {
    const closeButton = document.getElementById("closeAccountsModal");
    if (closeButton) {
        closeButton.addEventListener("click", closeAccountsModal);
    }
});



// MQTT АВТОРИЗАЦИЯ

// Функция для переключения видимости пароля в MQTT модальном окне
function toggleMqttPasswordVisibility() {
  const passwordInput = document.getElementById("mqttPassword");
  const passwordIcon = document.getElementById("mqttPasswordIcon");

  if (passwordInput && passwordIcon) {
    const isPasswordHidden = passwordInput.getAttribute("type") === "password";
    passwordInput.setAttribute("type", isPasswordHidden ? "text" : "password");
    passwordIcon.setAttribute("src", isPasswordHidden ? "../icon/Show_Passwd.svg" : "../icon/Hide_Passwd.svg");
    passwordIcon.setAttribute("alt", isPasswordHidden ? "Показать пароль" : "Скрыть пароль");
  }
}

// Показ модального окна MQTT авторизации
function showMqttAuthModal() {
  const modal = document.getElementById("mqttAuthModal");
  modal.style.display = "flex";

  // Сбрасываем состояние интерфейса
  const resetUIState = () => {
    const saveButton = document.getElementById("saveMqttAuth");
    saveButton.textContent = "Сохранить";
    saveButton.classList.remove("confirm-mode");

    const usernameInput = document.getElementById("mqttUsername");
    const passwordInput = document.getElementById("mqttPassword");
    usernameInput.disabled = false;
    passwordInput.disabled = false;
    usernameInput.value = "";
    passwordInput.value = "";

    // Устанавливаем тип поля пароля и обновляем иконку
    passwordInput.setAttribute("type", "password");

    // Сбрасываем иконку пароля
    const passwordIcon = document.getElementById("mqttPasswordIcon");
    if (passwordIcon) {
      passwordIcon.setAttribute("src", "../icon/Hide_Passwd.svg");
      passwordIcon.setAttribute("alt", "Показать пароль");
    }
  };

  resetUIState();

  // Загрузка данных с сервера
  fetch("/get-accounts-mqtt")
    .then((response) => response.json())
    .then((data) => {
      const account0 = data.find((acc) => acc.account === 0);
      if (account0) {
        document.getElementById("currentMqttUsername").value = account0.username;
      }

      // Управление состоянием резервного аккаунта
      const reserveAccount = data.find((acc) => acc.account === 1);
      if (reserveAccount) {
        const reserveAccountState = document.getElementById("reserveAccountState");
        const toggleReserveAccountButton = document.getElementById("toggleReserveAccount");

        if (reserveAccount.allow) {
          reserveAccountState.textContent = "Включён";
          reserveAccountState.className = "enabled";
          toggleReserveAccountButton.textContent = "Выключить";
        } else {
          reserveAccountState.textContent = "Выключен";
          reserveAccountState.className = "disabled";
          toggleReserveAccountButton.textContent = "Включить";
        }
      }
    })
    .catch((error) => {
      console.error("Ошибка загрузки данных:", error);
    });
}

// Обработчик для кнопки "Включить/Выключить"
document.getElementById("toggleReserveAccount").addEventListener("click", toggleReserveAccountState);

// Объявляем обработчики как глобальные переменные, чтобы они были доступны в resetReserveAccountButtonState
let handleConfirm, handleCancel;

function toggleReserveAccountState(e) {
  const toggleButton = document.getElementById("toggleReserveAccount");
  const reserveAccountState = document.getElementById("reserveAccountState");
  const isEnabled = reserveAccountState.textContent === "Включён";

  // Если аккаунт выключен и кнопка не в режиме подтверждения
  if (!isEnabled && !toggleButton.classList.contains("confirm-mode")) {
    // Активируем режим подтверждения
    toggleButton.textContent = "Подтверди правым кликом!";
    toggleButton.classList.add("confirm-mode");

    // Определяем обработчики для подтверждения и отмены
    handleConfirm = (e) => {
      e.preventDefault();
      updateReserveAccountStatus(true);
    };

    handleCancel = (e) => {
      if (e.button === 0) {
        // Левый клик
        resetReserveAccountButtonState();
      }
    };

    // Удаляем старые обработчики перед добавлением новых
    toggleButton.removeEventListener("contextmenu", handleConfirm);
    toggleButton.removeEventListener("click", handleCancel);

    // Добавляем новые обработчики
    toggleButton.addEventListener("contextmenu", handleConfirm);
    toggleButton.addEventListener("click", handleCancel);
  } else if (isEnabled) {
    // Если аккаунт включён, выключаем без подтверждения
    updateReserveAccountStatus(false);
  }
}

// Функция для сброса состояния кнопки
function resetReserveAccountButtonState() {
  const toggleButton = document.getElementById("toggleReserveAccount");
  toggleButton.textContent = "Включить";
  toggleButton.classList.remove("confirm-mode");

  // Удаляем обработчики событий
  toggleButton.removeEventListener("contextmenu", handleConfirm);
  toggleButton.removeEventListener("click", handleCancel);
}

// Функция для обновления статуса резервного аккаунта
function updateReserveAccountStatus(allow) {
  apiPostJson("/update-allow-mqtt", { allow })

    .then((response) => response.json())
    .then((data) => {
      if (data.success) {
        showPush("Статус резервного аккаунта обновлён", "#2196F3"); // Голубой
        // Сбрасываем состояние кнопки после успешного подтверждения
        resetReserveAccountButtonState();
        // Перезагружаем модальное окно для обновления состояния
        showMqttAuthModal();
      } else {
        showPush("Ошибка при обновлении статуса", "#ff4081"); // Розовый
      }
    })
    .catch((error) => {
      console.error("Ошибка при обновлении статуса:", error);
      showPush("Ошибка при обновлении статуса", "#ff4081"); // Розовый
    });
}

// Закрытие модального окна
function closeMqttAuthModal() {
  const modal = document.getElementById("mqttAuthModal");
  modal.style.display = "none";

  // Принудительный сброс состояния при закрытии
  const saveButton = document.getElementById("saveMqttAuth");
  saveButton.textContent = "Сохранить";
  saveButton.classList.remove("confirm-mode");
  document.getElementById("mqttUsername").disabled = false;
  document.getElementById("mqttPassword").disabled = false;

  // Удаляем все обработчики с кнопки
  const newSaveButton = saveButton.cloneNode(true);
  saveButton.parentNode.replaceChild(newSaveButton, saveButton);
  document.getElementById("saveMqttAuth").addEventListener("click", toggleSaveButtonState);
}

// Сохранение данных MQTT авторизации
function saveMqttAuth() {
  const username = document.getElementById("mqttUsername").value;
  const password = document.getElementById("mqttPassword").value;

  apiPostJson("/update-account-mqtt", { username, password })
 
    .then(async (response) => {
        const data = await response.text();
        if (response.ok) {
            showPush(data, "#2196F3"); // Голубой
            closeMqttAuthModal();      // Закрываем модальное окно только при успехе
        } else {
            showPush(data, "#ff4081"); // Красный
        }
    })
    .catch((error) => {
        console.error("Ошибка сохранения данных:", error);
        showPush("Ошибка соединения с сервером", "#ff4081");
        // Модальное окно НЕ закрываем!
    });
}

// Функция для изменения состояния кнопки
function toggleSaveButtonState() {
  const saveButton = document.getElementById("saveMqttAuth");
  const usernameInput = document.getElementById("mqttUsername");
  const passwordInput = document.getElementById("mqttPassword");

  // Объявляем обработчики как константы для последующего удаления
  const handleConfirm = (e) => {
    e.preventDefault();
    saveMqttAuth();
  };

  const handleCancel = (e) => {
    if (e.button === 0) {
      resetSaveButtonState();
    }
  };

  // Выносим сброс состояния в отдельную функцию
  const resetSaveButtonState = () => {
    saveButton.textContent = "Сохранить";
    saveButton.classList.remove("confirm-mode");
    usernameInput.disabled = false;
    passwordInput.disabled = false;

    // Удаляем обработчики событий
    saveButton.removeEventListener("contextmenu", handleConfirm);
    saveButton.removeEventListener("click", handleCancel);
  };

  // Проверка полей при первом нажатии
  if (!saveButton.classList.contains("confirm-mode")) {
	const username = usernameInput.value.trim();
    const passwd = passwordInput.value.trim();
	
    // Проверка на пустые поля
    if (!username || !passwd) {
      showPush("Все поля должны быть заполнены.", "#ff4081"); // Розовый
      return;
    }

	if (!validateInput(username)) {
		showPush("Логин содержит запрещённые символы!", "#ff4081"); // Розовый
		return;
		}
	if (!validateInput(passwd)) {
		showPush("Пароль содержит запрещённые символы!", "#ff4081"); // Розовый
		return;
	}
		
    // Активация режима подтверждения
    saveButton.textContent = "Подтверди правым кликом!";
    saveButton.classList.add("confirm-mode");
    usernameInput.disabled = true;
    passwordInput.disabled = true;

    // Удаляем старые обработчики перед добавлением новых
    saveButton.removeEventListener("contextmenu", handleConfirm);
    saveButton.removeEventListener("click", handleCancel);

    // Добавляем новые обработчики
    saveButton.addEventListener("contextmenu", handleConfirm);
    saveButton.addEventListener("click", handleCancel);
  }
}

document.getElementById('toggleMqttPasswordButton')?.addEventListener('click', toggleMqttPasswordVisibility);

// Привязка событий к элементам модального окна MQTT
document.getElementById("closeMqttAuthModal").addEventListener("click", closeMqttAuthModal);
document.getElementById("saveMqttAuth").addEventListener("click", toggleSaveButtonState);

// Привязка события к пункту меню "MQTT авторизация"
document.getElementById("accountsMQTT").addEventListener("click", showMqttAuthModal);



/* МОДАЛЬНОЕ ОКНО ВЫПОЛНИТЬ КОМАНДУ */

// Инициализация состояния при загрузке
document.addEventListener("DOMContentLoaded", function() {
  const passwordField = document.getElementById("commandPassword");
  passwordField.disabled = true; // Деактивируем поле "Пароль" при загрузке страницы
  updateUserExecutionRadioState();

	// Обработчик для кнопки переключения пароля
    const togglePasswordButton = document.querySelector(".command-toggle-password");
    if (togglePasswordButton) {
        togglePasswordButton.addEventListener("click", toggleCommandPasswordVisibility);
    }
	
	// Кнопка "Памятка с командами"
  const openCheatsheetButton = document.getElementById("openCommandCheatsheet");
  if (openCheatsheetButton) {
    openCheatsheetButton.addEventListener("click", openCommandCheatsheetModal);
  }

  // Крестик закрытия окна "Памятка с командами"
  const closeCheatsheetBtn = document.getElementById("closeCommandCheatsheetModal");
	if (closeCheatsheetBtn) {
	  closeCheatsheetBtn.addEventListener("click", closeCommandCheatsheetModal);
	}

	// Инициализация вкладок в модалке "Список полезных команд"
	initCheatsheetTabs();

	// Клик по кнопке "вставить" рядом с командой
	const cheatsheetModalEl = document.getElementById("commandCheatsheetModal");
	if (cheatsheetModalEl) {
	  cheatsheetModalEl.addEventListener("click", (e) => {
		const btn = e.target.closest(".cheat-insert-btn");
		if (!btn) return;

		e.preventDefault();
		e.stopPropagation();

		const row = btn.closest(".cheat-row");
		const codeEl = row && row.querySelector(".cheat-cmd code");
		const cmdText = codeEl ? codeEl.textContent.trim() : "";

		if (cmdText) {
		  const textarea = document.getElementById("commandText");
		  if (textarea) {
			textarea.value = cmdText; // вставляем команду
			textarea.focus();
			// курсор в конец
			textarea.selectionStart = textarea.selectionEnd = textarea.value.length;
		  }

		  // Автовыбор типа терминала по активной панели (CMD/PowerShell)
		  const panel = btn.closest(".cheatsheet-panel");
		  if (panel) {
			const isPs = panel.id.includes("powershell");
			const radio = document.querySelector(`input[name="terminalType"][value="${isPs ? "powershell" : "cmd"}"]`);
			if (radio) radio.checked = true;
		  }
		}

		// Закрываем только "Список полезных команд"
		closeCommandCheatsheetModal();
	  });
	}
});

// Открытие модального окна для выполнения команды
function openExecuteCommandModal() {
  document.getElementById("executeCommandModal").style.display = "flex";

  // Сброс состояния пароля при открытии
  const passwordInput = document.getElementById("commandPassword");
  const passwordIcon = document.getElementById("commandPasswordIcon");
  if (passwordInput && passwordIcon) {
    passwordInput.setAttribute("type", "password");
    passwordIcon.setAttribute("src", "../icon/Hide_Passwd.svg");
    passwordIcon.setAttribute("alt", "Показать пароль");
  }
}

// Закрытие модального окна для выполнения команды
function closeExecuteCommandModal() {
  document.getElementById("executeCommandModal").style.display = "none";
  // document.getElementById('executeCommandForm').reset(); // Сброс формы

  // Сброс состояния пароля
  const passwordInput = document.getElementById("commandPassword");
  const passwordIcon = document.getElementById("commandPasswordIcon");
  if (passwordInput && passwordIcon) {
    passwordInput.setAttribute("type", "password");
    passwordIcon.setAttribute("src", "../icon/Hide_Passwd.svg");
    passwordIcon.setAttribute("alt", "Показать пароль");
  }
}

// Открыть/закрыть "Памятка с командами"
function openCommandCheatsheetModal() {
  const modal = document.getElementById("commandCheatsheetModal");
  if (modal) {
    modal.style.display = "flex";
    setCheatsheetActiveTab("cmd"); // по умолчанию открываем вкладку CMD
  }
}

function closeCommandCheatsheetModal() {
  const modal = document.getElementById("commandCheatsheetModal");
  if (modal) modal.style.display = "none";
}

function initCheatsheetTabs() {
  const modal = document.getElementById("commandCheatsheetModal");
  if (!modal) return;

  const tabs = modal.querySelectorAll(".cheatsheet-tab");
  tabs.forEach((tabBtn) => {
    tabBtn.addEventListener("click", () => {
      const tabName = tabBtn.dataset.tab; // "cmd" или "powershell"
      setCheatsheetActiveTab(tabName);
    });
  });
}

function setCheatsheetActiveTab(name) {
  const modal = document.getElementById("commandCheatsheetModal");
  if (!modal) return;

  // Переключаем активные кнопки
  modal.querySelectorAll(".cheatsheet-tab").forEach((btn) => {
    btn.classList.toggle("active", btn.dataset.tab === name);
  });

  // Переключаем активные панели
  modal.querySelectorAll(".cheatsheet-panel").forEach((panel) => {
    panel.classList.remove("active");
  });
  const target = modal.querySelector(`#cheat-tab-${name}`);
  if (target) target.classList.add("active");
}

// Функция для переключения видимости пароля в модальном окне "Выполнить команду"
function toggleCommandPasswordVisibility() {
  const passwordInput = document.getElementById("commandPassword");
  const passwordIcon = document.getElementById("commandPasswordIcon");
  const userNameField = document.getElementById("commandUserName");

  if (passwordInput && passwordIcon) {
    const isPasswordHidden = passwordInput.getAttribute("type") === "password";
    passwordInput.setAttribute("type", isPasswordHidden ? "text" : "password");
    passwordIcon.setAttribute("src", isPasswordHidden ? "../icon/Show_Passwd.svg" : "../icon/Hide_Passwd.svg");
    passwordIcon.setAttribute("alt", isPasswordHidden ? "Показать пароль" : "Скрыть пароль");

    // Если поле "Имя пользователя" пустое, сохраняем стили для неактивного поля
    if (userNameField.value.trim() === "") {
      passwordInput.disabled = true;
    }
  }
}

// Функция для обновления состояния radio-кнопок
function updateUserExecutionRadioState() {
  const userName = document.getElementById("commandUserName").value.trim();
  const radios = document.querySelectorAll('#userExecutionFieldset input[type="radio"]');
  const isDisabled = userName === "";

  radios.forEach((radio) => {
    radio.disabled = isDisabled;

    // Если поле пустое, форсируем выбор "Выполнять для всех пользователей"
    if (isDisabled) {
      document.querySelector('#userExecutionFieldset input[value="true"]').checked = true;
    }
  });

  // Обновляем стили для всего fieldset
  const fieldset = document.getElementById("userExecutionFieldset");
  fieldset.style.opacity = isDisabled ? 0.6 : 1;
  fieldset.style.pointerEvents = isDisabled ? "none" : "auto";
}

// Привязка обработчика к кнопке закрытия
document.getElementById("closeExecuteCommandModal").addEventListener("click", closeExecuteCommandModal);

// Поддержка абзаца клавишей Enter в поле "textarea"
document.getElementById("commandText").addEventListener("keydown", function(event) {
    if (event.key === "Enter" && !event.shiftKey) {
        event.stopPropagation(); // Предотвращаем всплытие
        return; // Обычное поведение textarea (перенос строки)
    }
});

// Поддержка Tab в поле "textarea" (отступ вместо переключения фокуса)
document.getElementById("commandText").addEventListener("keydown", function(event) {
    if (event.key === "Tab") {
        // Блокировка стандартного поведения клавиши Tab
        event.preventDefault();
        event.stopPropagation();
        if (event.stopImmediatePropagation) event.stopImmediatePropagation();

        const textarea = this;
        const start = textarea.selectionStart;
        const end = textarea.selectionEnd;

        // Отступ: 4 пробела
        const tabChar = "    ";

		// Вставка только пробелов
        textarea.value = textarea.value.substring(0, start) + tabChar + textarea.value.substring(end);

        // Сдвиг курсора сразу после вставленных пробелов
        textarea.selectionStart = textarea.selectionEnd = start + tabChar.length;
    }
});

// Обработка отправки формы
document.getElementById("executeCommandForm").addEventListener("submit", function(event) {
  event.preventDefault();

  // Получаем выбранных клиентов (true в checkboxStates)
  const selectedClients = Object.entries(checkboxStates)
    .filter(([key, state]) => state && key.startsWith("checkbox_"))
    .map(([clientId]) => clientId.replace("checkbox_", ""));

  if (selectedClients.length === 0) {
    showPush("Выберите хотя бы одного клиента.", "#ff4081"); // Розовый
    return;
  }

  // Формируем константы перед отправкой
  const terminalType = document.querySelector('input[name="terminalType"]:checked').value;
  const commandText = document.getElementById("commandText").value.trim();
  const workingFolder = document.getElementById("workingFolder").value.trim();
  const runUserScope = document.querySelector('input[name="runUserScope"]:checked').value === "true";
  const commandUserName = document.getElementById("commandUserName").value.trim();
  const commandPassword = document.getElementById("commandPassword").value.trim();
  const highestPrivileges = document.getElementById("highestPrivileges").checked;

  // Проверка поля на пустоту
  if (!commandText) {
    showPush("Поле команды не может быть пустым.", "#ff4081"); // Розовый
    return;
  }

  // Переопределяем runUserScope если имя пользователя не указано
  let finalRunUserScope = runUserScope;
  if (commandUserName === "") {
    finalRunUserScope = true;
  }

  // Формируем данные для отправки
  const requestData = {
    client_ids: selectedClients,
    terminal_command: terminalType,
    command: commandText,
    working_folder: workingFolder,
    run_whether_user_is_logged_on_or_not: finalRunUserScope,
    user_name: commandUserName,
    user_password: commandPassword,
    run_with_highest_privileges: highestPrivileges,
  };

  // Отправка POST-запроса на сервер
	apiPostJson("/send-terminal-command", requestData)
    .then((response) => response.json())
    .then((data) => {
      // Проверяем статус ответа от сервера
      if (data.status === "Ошибка") {
        showPush(data.message, "#ff4d4d"); // Красный
      } else if (data.status === "Успех") {
        showPush(data.message, "#4CAF50"); // Зелёный
        closeExecuteCommandModal(); // Закрываем модальное окно только при успехе
      }
    })
    .catch((error) => {
      showPush("Не удалось отправить команду.", "#ff4081"); // Розовый
    });
});

// Обработчик изменений в поле "Имя пользователя"
document.getElementById("commandUserName").addEventListener("input", function(event) {
  const passwordField = document.getElementById("commandPassword");
  const userName = event.target.value.trim();

  // Обновление состояния поля пароля
  if (userName !== "") {
    passwordField.disabled = false;
  } else {
    passwordField.disabled = true;
    passwordField.value = "";
  }

  // Обновление состояния radio-кнопок
  updateUserExecutionRadioState();
});

// При открытии модального окна проверяем состояние поля "Имя пользователя"
document.getElementById("executeCommandModal").addEventListener("click", function() {
  const userNameField = document.getElementById("commandUserName");
  const passwordField = document.getElementById("commandPassword");
  if (userNameField.value.trim() === "") {
    passwordField.disabled = true; // Деактивируем поле "Пароль", если имя пользователя пустое
  }
});

// Функция для вызова из контекстного меню
function runCommand() {
  openExecuteCommandModal();
}



/* МОДАЛЬНОЕ ОКНО УСТАНОВКА ПО */

// Глобальные переменные
let uploadXHR = null; // Для отмены загрузки
let uploadedFilePath = null; // Для хранения пути загруженного файла
let isUploading = false; // Флаг для отслеживания состояния загрузки

let currentUploadFile = null; // Добавляем для отслеживания текущего файла
let lastUploadPercent = -1; // Добавляем для хранения последнего процента

// Возвращает true, если в данный момент мы НЕ МОЖЕМ начать новую загрузку
function isBlocked() {
  return isUploading || uploadedFilePath !== null;
}

// Инициализация состояния при загрузке
document.addEventListener("DOMContentLoaded", function() {
  const installPasswordField = document.getElementById("installPassword");
  installPasswordField.disabled = true;
  updateInstallUserExecutionRadioState();
});

// Функция для открытия модального окна установки ПО
function openInstallProgramModal() {
  document.getElementById("installProgramModal").style.display = "flex";

  // Если идет загрузка, восстанавливаем состояние
  if (isUploading && currentUploadFile) {
    document.getElementById("dropText").style.display = "none";
    document.getElementById("fileNameText").textContent = currentUploadFile.name;
    document.getElementById("fileNameText").style.display = "block";
    document.getElementById("uploadProgress").style.display = "block";
    document.getElementById("uploadProgress").textContent = `Загружено ${lastUploadPercent}%`;
    document.getElementById("cancelUploadText").style.display = "block";
  }
}

// Функция для закрытия модального окна установки ПО
function closeInstallProgramModal() {
  document.getElementById("installProgramModal").style.display = "none";
}

// Привязка обработчика к кнопке закрытия
document.getElementById("closeInstallProgramModal").addEventListener("click", closeInstallProgramModal);

// Функция для вызова из контекстного меню или кнопки
function installProgram() {
  openInstallProgramModal();
}

// Функция для переключения видимости пароля в модальном окне "Установка ПО"
function toggleInstallPasswordVisibility() {
  const passwordInput = document.getElementById("installPassword");
  const passwordIcon = document.getElementById("installPasswordIcon");
  const userNameField = document.getElementById("installUserName");

  if (passwordInput && passwordIcon) {
    const isPasswordHidden = passwordInput.getAttribute("type") === "password";
    passwordInput.setAttribute("type", isPasswordHidden ? "text" : "password");
    passwordIcon.setAttribute("src", isPasswordHidden ? "../icon/Show_Passwd.svg" : "../icon/Hide_Passwd.svg");
    passwordIcon.setAttribute("alt", isPasswordHidden ? "Показать пароль" : "Скрыть пароль");

    // Если поле "Имя пользователя" пустое, сохраняем стили для неактивного поля
    if (userNameField.value.trim() === "") {
      passwordInput.disabled = true;
    }
  }
}

// Функция для обновления состояния radio-кнопок и поля пароля
function updateInstallUserExecutionRadioState() {
  const userName = document.getElementById("installUserName").value.trim();
  const radios = document.querySelectorAll('#installUserExecutionFieldset input[type="radio"]');
  const passwordField = document.getElementById("installPassword");
  const isDisabled = userName === "";

  radios.forEach((radio) => {
    radio.disabled = isDisabled;
    if (isDisabled) {
      document.querySelector('#installUserExecutionFieldset input[value="true"]').checked = true;
    }
  });

  // Обновляем стили для всего fieldset
  const fieldset = document.getElementById("installUserExecutionFieldset");
  fieldset.style.opacity = isDisabled ? 0.6 : 1;
  fieldset.style.pointerEvents = isDisabled ? "none" : "auto";

  // Обновляем состояние поля пароля
  if (isDisabled) {
    passwordField.disabled = true;
    passwordField.value = "";
  } else {
    passwordField.disabled = false;
  }
}

// Обработчик для кнопки переключения пароля в установке ПО
document.addEventListener('DOMContentLoaded', function() {
    const toggleInstallPasswordButton = document.getElementById('toggleInstallPasswordButton');
    if (toggleInstallPasswordButton) {
        toggleInstallPasswordButton.addEventListener('click', toggleInstallPasswordVisibility);
    }
});

// Обработчик изменений в поле "Имя пользователя" для "Установка ПО"
document.getElementById("installUserName").addEventListener("input", function(event) {
  updateInstallUserExecutionRadioState();
});

// Обработка Drag-and-drop
const dropArea = document.getElementById("dropArea");
const fileInput = document.getElementById("fileInput");
const dropText = document.getElementById("dropText");

// Выбор файла по клику
dropArea.addEventListener("click", () => {
  if (isBlocked()) return; // <-- блокируем клик
  fileInput.click();
});

// Выбор файла методом перетягивания на панель
fileInput.addEventListener("change", (event) => {
  if (isBlocked()) {
    // сбрасываем ненужный выбор, чтобы input.value не хранил новый файл
    event.target.value = "";
    return;
  }
  const file = event.target.files[0];
  if (file) uploadFile(file);
});

dropArea.addEventListener("dragover", (event) => {
  event.preventDefault();

  if (isBlocked()) {
    dropArea.classList.add("drag-over");
    event.dataTransfer.dropEffect = "none"; // <-- это ключ!
    return;
  }

  dropText.textContent = "Отпустите кнопку мыши";
  event.dataTransfer.dropEffect = "copy"; // Разрешаем копирование
});

dropArea.addEventListener("dragleave", () => {
  dropArea.classList.remove("drag-over");

  if (isBlocked()) return;

  dropText.textContent = "Перетащите исполняемый файл на панель или кликнете сюда";
});

dropArea.addEventListener("drop", (event) => {
  event.preventDefault();
  dropArea.classList.remove("drag-over");

  if (isBlocked()) return;

  const file = event.dataTransfer.files[0];
  if (file) uploadFile(file);
});

// Функция для загрузки файла на сервер
function uploadFile(file) {
  const formData = new FormData();
  formData.append("file", file);

  // Сохраняем текущий файл и состояние
  currentUploadFile = file;
  isUploading = true;
  lastUploadPercent = 0;
  dropArea.classList.add("blocked");

  // Показываем информацию о загрузке
  document.getElementById("dropText").style.display = "none";
  document.getElementById("fileNameText").textContent = file.name;
  document.getElementById("fileNameText").style.display = "block";
  document.getElementById("uploadProgress").textContent = `Загружено 0%`;
  document.getElementById("uploadProgress").style.display = "block";
  document.getElementById("cancelUploadText").style.display = "block";

  uploadXHR = new XMLHttpRequest();
  uploadXHR.open("POST", "/upload-file-QUIC", true);
  const csrf = getCsrfSyncOrThrow();
  uploadXHR.setRequestHeader("X-CSRF-Token", csrf);

  uploadXHR.upload.onprogress = (event) => {
    if (event.lengthComputable) {
      const percentComplete = Math.round((event.loaded / event.total) * 100);
      lastUploadPercent = percentComplete;
      document.getElementById("uploadProgress").textContent = `Загружено ${percentComplete}%`;
    }
  };

  // Обработчик onload
  uploadXHR.onload = function() {
  try {
    // Подхват ротации токена из ответа XHR
    const newTok = uploadXHR.getResponseHeader("X-CSRF-Token");
    if (newTok) window.CSRF_TOKEN = newTok;
  } catch (_) {}

  if (uploadXHR.status === 403) {
    // Обновим токен и сообщим админу
    fetchCsrfToken().finally(() => {
      showPush("Сессия обновлена. Повторите загрузку файла.", "#2196F3"); // Голубой
    });

    // Сброс визуального состояния
    isUploading = false;
    currentUploadFile = null;
    lastUploadPercent = -1;

    document.getElementById("uploadProgress").style.display = "none";
    document.getElementById("cancelUploadText").style.display = "none";
    document.getElementById("fileNameText").style.display = "none";
    document.getElementById("dropText").style.display = "block";

  } else if (uploadXHR.status === 200) {
    try {
      const response = JSON.parse(uploadXHR.responseText);
      if (response.status === "Успех") {
        showPush("Файл успешно загружен на сервер", "#4CAF50"); // Зелёный
        uploadedFilePath = response.filePath;
        document.getElementById("dropText").textContent = file.name;

        const postUploadCancelTextElement = document.getElementById("postUploadCancelText");
        if (postUploadCancelTextElement) {
          postUploadCancelTextElement.style.display = "block";
        }
      } else {
        showPush(response.message || "Ошибка загрузки файла", "#ff4d4d"); // Красный
      }
    } catch (e) {
      showPush("Ошибка парсинга ответа сервера", "#ff4d4d"); // Красный
    }

    isUploading = false;
    currentUploadFile = null;
    lastUploadPercent = -1;
    document.getElementById("uploadProgress").style.display = "none";
    document.getElementById("cancelUploadText").style.display = "none";

  } else {
    showPush("Ошибка загрузки файла", "#ff4d4d"); // Красный

    isUploading = false;
    currentUploadFile = null;
    lastUploadPercent = -1;
    document.getElementById("fileNameText").style.display = "none";
    document.getElementById("dropText").style.display = "block";
  }
};

  // Полный обработчик onerror
  uploadXHR.onerror = function() {
    isUploading = false;
    currentUploadFile = null;
    lastUploadPercent = -1;

    showPush("Ошибка загрузки файла: проблема с соединением", "#ff4d4d"); // Красный
    document.getElementById("uploadProgress").style.display = "none";
    document.getElementById("cancelUploadText").style.display = "none";
    document.getElementById("fileNameText").style.display = "none";
    document.getElementById("dropText").style.display = "block";
  };

  uploadXHR.send(formData);
}

// Обработчик для чекбокса "Только скачать"
const onlyDownloadCheckbox = document.getElementById("onlyDownload");
const launchKeysInput = document.getElementById("launchKeys");
const installUserNameInput = document.getElementById("installUserName");
const installHighestPrivilegesCheckbox = document.getElementById("installHighestPrivileges");
const notDeleteAfterInstallationCheckbox = document.getElementById("notDeleteAfterInstallation");

// Функция для обновления состояния полей в зависимости от галочки "Только скачать"
function updateFieldsBasedOnOnlyDownload() {
  const isOnlyDownload = onlyDownloadCheckbox.checked;

  launchKeysInput.disabled = isOnlyDownload;
  installUserNameInput.disabled = isOnlyDownload;
  installHighestPrivilegesCheckbox.disabled = isOnlyDownload;
  notDeleteAfterInstallationCheckbox.disabled = isOnlyDownload;

  if (isOnlyDownload) {
    launchKeysInput.value = "";
    installUserNameInput.value = "";
    installHighestPrivilegesCheckbox.checked = false;
    notDeleteAfterInstallationCheckbox.checked = false;

    // Явно обновляем состояние полей "Выполнение для пользователей:" и "Пароль (опционально):"
    updateInstallUserExecutionRadioState();
  }
}

// Обработчик для чекбокса "Только скачать"
onlyDownloadCheckbox.addEventListener("change", updateFieldsBasedOnOnlyDownload);

// Обработка отправки формы
document.getElementById("installProgramForm").addEventListener("submit", function(event) {
  event.preventDefault();

  // Проверка, загружен ли файл
  if (!uploadedFilePath) {
    showPush("Файл ещё не загружен!", "#ff4081"); // Розовый
    return;
  }

  // Получаем выбранных клиентов
  const selectedClients = Object.entries(checkboxStates)
    .filter(([key, state]) => state && key.startsWith("checkbox_"))
    .map(([clientId]) => clientId.replace("checkbox_", ""));

  if (selectedClients.length === 0) {
    showPush("Выберите хотя бы одного клиента.", "#ff4081"); // Розовый
    return;
  }

  // Собираем данные формы
  const onlyDownload = document.getElementById("onlyDownload").checked;
  const downloadPath = document.getElementById("downloadPath").value.trim();
  let launchKeys = document.getElementById("launchKeys").value.trim();
  let runUserScope = document.querySelector('input[name="installRunUserScope"]:checked').value === "true";
  let installUserName = document.getElementById("installUserName").value.trim();
  let installPassword = document.getElementById("installPassword").value.trim();
  let highestPrivileges = document.getElementById("installHighestPrivileges").checked;
  let notDeleteAfterInstallation = document.getElementById("notDeleteAfterInstallation").checked;

  // Формируем "DownloadRunPath"
  let downloadRunPath;
  const fileName = uploadedFilePath.split("/").pop(); // Получаем имя файла
  if (downloadPath) {
    downloadRunPath = `${downloadPath}\\${fileName}`.replace(/\\+/g, "\\");
  } else {
    downloadRunPath = fileName; // Только имя файла, если путь не указан
  }

  // Если "OnlyDownload" == true, корректируем значения
  if (onlyDownload) {
    launchKeys = "";
    installUserName = "";
    installPassword = "";
    runUserScope = true;
    highestPrivileges = false;
    notDeleteAfterInstallation = false;
  } else if (installUserName === "") {
    runUserScope = true;
  }

  // Формируем данные для отправки
  const requestData = {
    client_ids: selectedClients,
    OnlyDownload: onlyDownload,
    DownloadRunPath: downloadRunPath,
    ProgramRunArguments: launchKeys,
    RunWhetherUserIsLoggedOnOrNot: runUserScope,
    UserName: installUserName,
    UserPassword: installPassword,
    RunWithHighestPrivileges: highestPrivileges,
    NotDeleteAfterInstallation: notDeleteAfterInstallation,
  };

  // Отправка POST-запроса на сервер
	apiPostJson("/send-install-QUIC-program", requestData)
    .then((response) => response.json())
    .then((data) => {
      if (data.status === "Ошибка") {
        showPush(data.message, "#ff4d4d"); // Красный
      } else if (data.status === "Успех") {
        showPush(data.message, "#4CAF50"); // Зелёный
        resetDropArea(); // Очищаем только Drag-and-drop панель
        closeInstallProgramModal(); // Закрываем модальное окно только при успехе
      }
    })
    .catch((error) => {
      showPush("Не удалось отправить запрос на установку.", "#ff4081"); // Розовый
    });
});

// Функция для отображения модального окна подтверждения отмены загрузки
function showConfirmCancelUploadModal(message, onConfirm) {
  const modal = document.getElementById("confirmCancelUploadModal");
  const msgEl = document.getElementById("confirmCancelUploadMessage");
  const confirmButton = document.getElementById("confirmCancelReportButton");
  const closeButton = document.getElementById("closeConfirmCancelUploadModal");

  msgEl.textContent = message;
  modal.style.display = "flex";

  function confirmHandler() {
    onConfirm();
    modal.style.display = "none";
    cleanup();
  }

  function cancelHandler() {
    modal.style.display = "none";
    cleanup();
  }

  function handleKeyPress(event) {
    if (event.key === "Enter") {
      confirmHandler();
    }
  }

  function handleEscPress(event) {
    if (event.key === "Escape") {
      cancelHandler();
    }
  }

  function cleanup() {
    confirmButton.removeEventListener("click", confirmHandler);
    closeButton.removeEventListener("click", cancelHandler);
    confirmButton.removeEventListener("keydown", handleKeyPress);
    document.removeEventListener("keydown", handleEscPress);
  }

  confirmButton.addEventListener("click", confirmHandler);
  closeButton.addEventListener("click", cancelHandler);

  // Добавляем обработчик Enter для кнопки
  confirmButton.addEventListener("keydown", handleKeyPress);

  // Добавляем обработчик Esc для всего документа
  document.addEventListener("keydown", handleEscPress);

  // Устанавливаем фокус на кнопку "Отменить"
  confirmButton.focus();
}

// Функция для удаления файла на сервере
async function deleteFileOnServer(filename) {
  try {
    const resp = await apiPostJson("/delete-file-QUIC", { filename });
    const data = await resp.json();
	
    if (data.status === "Успех") {
      showPush(data.message, "#2196F3"); // Голубой
      return true;
    } else {
      showPush(data.message || "Ошибка отмены загрузки", "#ff4d4d"); // Красный
      return false;
    }
  } catch (error) {
    showPush("Ошибка при отправке запроса на отмену загрузки", "#ff4d4d"); // Красный
    return false;
  }
}

// Функция для сброса состояния загрузки
function resetUploadState() {
  document.getElementById("fileNameText").style.display = "none";
  document.getElementById("uploadProgress").style.display = "none";
  document.getElementById("cancelUploadText").style.display = "none";
  document.getElementById("dropText").style.display = "block";
  document.getElementById("dropText").textContent = "Перетащите исполняемый файл на панель или кликнете сюда";

  const postUploadCancelTextElement = document.getElementById("postUploadCancelText");
    if (postUploadCancelTextElement) {
        postUploadCancelTextElement.style.display = "none";
    }
	
  uploadedFilePath = null;
  isUploading = false;
  currentUploadFile = null;
  lastUploadPercent = -1;
  uploadXHR = null;
  fileInput.value = "";
}

// Функция очистки Drag-and-drop панели после успешной отправки JSON серверу
function resetDropArea() {
  document.getElementById("fileNameText").style.display = "none";
  document.getElementById("uploadProgress").style.display = "none";
  document.getElementById("cancelUploadText").style.display = "none";
  document.getElementById("dropText").style.display = "block";
  document.getElementById("dropText").textContent = "Перетащите исполняемый файл на панель или кликнете сюда";
  
  const postUploadCancelTextElement = document.getElementById("postUploadCancelText");
    if (postUploadCancelTextElement) {
        postUploadCancelTextElement.style.display = "none";
    }
	
  uploadedFilePath = null;
  currentUploadFile = null;
  lastUploadPercent = -1;
  fileInput.value = "";
}


// Обработчик отмены загрузки (для всех случаев)
dropArea.addEventListener("contextmenu", async (event) => {
  event.preventDefault();

  // Если нет активной загрузки и нет загруженного файла
  if (!isUploading && !uploadedFilePath && !currentUploadFile) {
    return;
  }

  // Определяем имя файла для сообщения
  let fileName = "";
  if (currentUploadFile) {
    fileName = currentUploadFile.name;
  } else if (uploadedFilePath) {
    fileName = uploadedFilePath.split("/").pop();
  }

  // Формируем сообщение в зависимости от состояния
  let message;
  if (isUploading) {
    message = `"${fileName}"?`;
  } else {
    message = `"${fileName}"?`;
  }

  showConfirmCancelUploadModal(message, async () => {
    // Отменяем загрузку, если она идет
    if (isUploading && uploadXHR) {
      uploadXHR.abort();
    }

    // Отправляем запрос на удаление файла на сервере
    let deleteSuccess = true;
    if (fileName) {
      deleteSuccess = await deleteFileOnServer(fileName);
    }

    // Сбрасываем состояние только если удаление прошло успешно или файла не было
    if (deleteSuccess || !fileName) {
      resetUploadState();
    } else {
      // Если не удалось удалить, но файл был загружен - оставляем его
      if (uploadedFilePath) {
        document.getElementById("dropText").textContent = fileName;
      }
    }
  });
});



// МОДАЛЬНОЕ ОКНО "Отчёт"

// Функция для открытия вкладки
function openReportTab(event, tabName) {
  const tabContents = document.querySelectorAll(".report-tab-content");
  tabContents.forEach((tab) => {
    tab.style.display = "none";
    tab.classList.remove("active");
  });

  const tabButtons = document.querySelectorAll(".report-tab-button");
  tabButtons.forEach((button) => {
    button.classList.remove("active");
  });

  document.getElementById(tabName).style.display = "block";
  document.getElementById(tabName).classList.add("active");
  event.currentTarget.classList.add("active");



  if (tabName === "cmdReport") {
    loadCmdDates();
    CmdAutoUpdater.start(); // Запускаем автообновление
    InstallAutoUpdater.stop();
  } else if (tabName === "installReport") {
    loadInstallDates();
    InstallAutoUpdater.start();
    CmdAutoUpdater.stop(); // Останавливаем автообновление
  }
}

// Закрытие модального окна отчётов
function closeReportModal() {
  document.getElementById("reportModal").style.display = "none";
  CmdAutoUpdater.stop(); // Останавливаем автообновление при закрытии модального окна
  InstallAutoUpdater.stop();
}

// Открытие модального окна отчётов
function openReportModal() {
    document.getElementById("reportModal").style.display = "flex";
    
    // Создаем фейковое событие для инициализации
    const fakeEvent = {
        currentTarget: document.getElementById("installReportTab")
    };
    openReportTab(fakeEvent, "installReport");
}

// Функция для парсинга даты из строки
function parseCustomDate(dateString) {
    const [datePart, timeAndMs] = dateString.split('(');
    const [day, month, year] = datePart.split('.');
    const [timePart] = timeAndMs.split(')');
    const [hours, minutes, seconds] = timePart.split(':');
    const fullYear = parseInt(year) < 100 ? 2000 + parseInt(year) : parseInt(year);
    return new Date(fullYear, month - 1, day, hours, minutes, seconds);
}

// Функция для форматирования даты
function formatDate(dateString) {
    const date = parseCustomDate(dateString);
    const day = String(date.getDate()).padStart(2, '0');
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const year = String(date.getFullYear()).slice(2);
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');
    const seconds = String(date.getSeconds()).padStart(2, '0');
    return `${day}.${month}.${year} (${hours}:${minutes}:${seconds})`;
}

// Функция вычисления размера загруженного файла на сервер
function formatFileSize(bytes) {
  if (bytes === undefined || bytes === null || isNaN(bytes)) return '—';
  const b = Number(bytes);

  if (b < 1024) {
    // Меньше килобайта — показать в байтах
    return b + ' байт' + (b !== 1 ? 'а' : '');
  } else if (b < 1024 * 1024) {
    // Килобайты
    return (b / 1024).toFixed(2) + ' КБ';
  } else if (b < 1024 * 1024 * 1024) {
    // Мегабайты
    return (b / (1024 * 1024)).toFixed(2) + ' МБ';
  } else {
    // Гигабайты
    return (b / (1024 * 1024 * 1024)).toFixed(2) + ' ГБ';
  }
}

function adjustHeaderPadding(containerClass) {
    const bodyContainer = document.querySelector(`.${containerClass} .clients-table-body-container`);
    const headerContainer = document.querySelector(`.${containerClass} .clients-table-header-container`);
    if (!bodyContainer || !headerContainer) return;
    const isChromium = /Chrome/.test(navigator.userAgent) && /Google Inc/.test(navigator.vendor);
    if (bodyContainer.scrollHeight > bodyContainer.clientHeight && isChromium) {
        headerContainer.style.paddingRight = "18px";
    } else {
        headerContainer.style.paddingRight = "3px";
    }
}

// Функция для загрузки дат создания команд
function loadDates(url, selectId, reportType) {
    fetch(url, { method: "GET", credentials: "same-origin" })
	
    .then((response) => response.json())
    .then((data) => {
        const select = document.getElementById(selectId);
        if (!data || data.length === 0) {
            select.innerHTML = '<option value="">Нет запросов</option>';
            return;
        }
        data.sort((a, b) => parseCustomDate(b.Date_Of_Creation) - parseCustomDate(a.Date_Of_Creation));
        select.innerHTML = "";
        data.forEach((item) => {
            const option = document.createElement("option");
            option.value = item.Date_Of_Creation;
            option.textContent = formatDate(item.Date_Of_Creation);
            select.appendChild(option);
        });
        if (data.length > 0) {
            if (reportType === 'cmd') {
                loadCmdReport();
            } else if (reportType === 'install') {
                loadInstallReport();
            }
        }
    })
    .catch((error) => {
        console.error("Ошибка загрузки дат:", error);
    });
}

// Функция для удаления всего запроса с подтверждением
function deleteSelectedRequest(url, selectId, reportType) {
    const selectElement = document.getElementById(selectId);
    const selectedDate = selectElement.value;
    if (!selectedDate) {
        showPush("Выберите запрос для удаления!", "#ff4081");
        return;
    }
    const selectedOptionText = selectElement.options[selectElement.selectedIndex].text;
    showConfirmModal(`От "${selectedOptionText}"?`, function() {
        
	apiPostJson(url, { Date_Of_Creation: selectedDate })
		
        .then((response) => response.json())
        .then((result) => {
            if (result.status === "Успех") {
                showPush(result.message, "#4CAF50"); // Зелёный
                if (reportType === 'cmd') {
                    loadCmdDates();
                    document.getElementById("cmdDetails").innerHTML = "";
                    document.getElementById("clientsTableBody").innerHTML = "";
                } else if (reportType === 'install') {
                    loadInstallDates();
                    document.getElementById("installDetails").innerHTML = "";
                    document.getElementById("installClientsTableBody").innerHTML = "";
                }
            } else {
                showPush("Ошибка: " + result.message, "#ff4d4d"); // Красный
            }
        })
        .catch((error) => {
            console.error("Ошибка:", error);
            showPush("Ошибка: " + error.message, "#ff4d4d"); // Красный
        });
    });
}


// "По Установкам ПО"

// Вызов загрузки дат для вкладки "По установкам ПО"
function loadInstallDates() {
    loadDates("/get-QUIC-report", "installDateSelect", "install");
}

// Вызов удаления всего запроса с подтверждением
function deleteSelectedInstall() {
    deleteSelectedRequest("/delete-by-date-QUIC-report", "installDateSelect", "install");
}

function loadInstallReport() {
  const selectedDate = document.getElementById('installDateSelect').value;
  fetch('/get-QUIC-report', { method: 'GET', credentials: "same-origin" })
  
    .then(response => response.json())
    .then(data => {
      if (!data || data.length === 0) {
        document.getElementById('installDetails').innerHTML = '';
        document.getElementById('installClientsTableBody').innerHTML = '';
        document.getElementById('installDateSelect').innerHTML = '<option value="">Нет запросов</option>';
        showPush("Нет доступных запросов.", '#ff4081'); // Розовый
        return;
      }
      const selectedCommand = data.find(item => item.Date_Of_Creation === selectedDate);
      if (selectedCommand) {
        displayInstallDetails(selectedCommand);
        displayInstallClients(selectedCommand.ClientID_QUIC);
		
        const detailsDiv = document.getElementById('installDetails');
        const fieldset = document.querySelector('.install-fieldset-fixed');
		const savedState = sessionStorage.getItem('installDetailsState') || 'collapsed';
		
        if (savedState === 'expanded') {
		  fieldset.classList.add('expanded');
		  fieldset.classList.remove('collapsed');
		  document.getElementById('toggleInstallDetails').textContent = '-';
		} else {
		  fieldset.classList.add('collapsed');
		  fieldset.classList.remove('expanded');
		  document.getElementById('toggleInstallDetails').textContent = '+';
		}
		setInstallClientsTableHeight();
		adjustHeaderPadding('install-clients-table-container');
      } else {
        document.getElementById('installDetails').innerHTML = '';
        document.getElementById('installClientsTableBody').innerHTML = '';
        loadInstallDates();
        showPush("Клиентов не осталось, запрос удалён", '#4CAF50'); // Зелёный
      }
    })
    .catch(error => {
      console.error('Ошибка загрузки отчёта:', error);
      showPush("Ошибка: " + error.message, '#ff4d4d');  // Красный
    });
}

// Анимация и работа кнопки "+/-" для Установка ПО
function toggleInstallDetails() {
  const fieldset = document.querySelector('.install-fieldset-fixed');
  const details = document.getElementById('installDetails');
  const toggleButton = document.getElementById('toggleInstallDetails');

  if (details.dataset.animating === '1') return;
  details.dataset.animating = '1';

  const isExpanded = fieldset.classList.contains('expanded');

  if (isExpanded) {
    // Сворачивание — обе анимации синхронно
    sessionStorage.setItem('installDetailsState', 'collapsed');
    setInstallClientsTableHeight();
    adjustHeaderPadding('install-clients-table-container');

	// Детали к 0px (плавное сворачивание)
    const startHeight = details.offsetHeight;
    details.style.maxHeight = startHeight + 'px';
    void details.getBoundingClientRect();
    details.style.maxHeight = '0px';
    toggleButton.textContent = '+';

    const onEnd = (e) => {
      if (e.propertyName !== 'max-height') return;
      details.removeEventListener('transitionend', onEnd);
	  
	  // Только после окончания — переключаем класс "рамки"
      fieldset.classList.remove('expanded');
      fieldset.classList.add('collapsed');
	  
	  // Чистим инлайн
      details.style.maxHeight = '';
	  
	  // Финальная корректировка шапки (после анимации)
      setTimeout(() => adjustHeaderPadding('install-clients-table-container'), 0);
      details.dataset.animating = '0';
    };
    details.addEventListener('transitionend', onEnd);
  } else {
    // Разворачивание — обе анимации синхронно
    sessionStorage.setItem('installDetailsState', 'expanded');
    setInstallClientsTableHeight();
    adjustHeaderPadding('install-clients-table-container');

	// Детали к целевому значению через CSS
    fieldset.classList.add('expanded');
    fieldset.classList.remove('collapsed');
    details.style.maxHeight = ''; // позволяем правилу .expanded #cmdDetails анимировать до 185px
    toggleButton.textContent = '-';

    const onEnd = (e) => {
      if (e.propertyName !== 'max-height') return;
      details.removeEventListener('transitionend', onEnd);
      setTimeout(() => adjustHeaderPadding('install-clients-table-container'), 0);
      details.dataset.animating = '0';
    };
    details.addEventListener('transitionend', onEnd);
  }
}

function setInstallClientsTableHeight() {
  const tableContainer = document.querySelector('.install-clients-table-container');
  if (!tableContainer) return;
  const state = sessionStorage.getItem('installDetailsState') || 'collapsed';
  if (state === 'expanded') {
    tableContainer.style.maxHeight = '294px';
  } else {
    tableContainer.style.maxHeight = '450px';
  }
}

// Нормализация строки
function normalizeStr(s) {
  return (s === undefined || s === null) ? '' : String(s).trim();
}

// Сигнатура состояния статуса клиента (чтобы не перерисовывать без надобности)
function getInstallStatusSig(cd) {
  return [
    normalizeStr(cd.Answer),
    normalizeStr(cd.QUIC_Execution),
    normalizeStr(cd.Attempts),
    normalizeStr(cd.Description)
  ].join('|');
}

// Экранирование текста для безопасной вставки в data-атрибут
function escapeAttr(s) {
  if (s === undefined || s === null) return '';
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

// Генерация HTML статуса для "По установкам ПО"
function buildInstallStatusHTML(clientData) {
  const hasAnswer = !!(clientData && clientData.Answer && clientData.Answer.trim());
  if (!hasAnswer) {
    return '<span class="pending">—</span>';
  }

  const status = (clientData.QUIC_Execution || '').trim();
  const attempts = (clientData.Attempts || '').trim();
  const descr = (clientData.Description || '').trim();

  if (status === 'Успех') {
    const tip = `Попытка: ${attempts || '—'}\n\nОписание: ${descr || '—'}`;
    // добавили tt-wide
    return `<span class="done tt-btn tt-wide" data-tt="${escapeAttr(tip)}">✓ - ${clientData.Answer}</span>`;
  } else if (status === 'Ошибка') {
    const tip = `Неудачных попыток: ${attempts || '—'}\n\nОписание: ${descr || '—'}`;
    // добавили tt-wide
    return `<span class="error tt-btn tt-wide" data-tt="${escapeAttr(tip)}">X - ${clientData.Answer}</span>`;
  }

  // Фолбек для старых записей
  return `<span class="done">✓ - ${clientData.Answer}</span>`;
}

function displayInstallDetails(command) {
  const detailsDiv = document.getElementById('installDetails');
  const quicCommand = JSON.parse(command.QUIC_Command);
  const fileSizeHuman = formatFileSize(command.File_Size_Bytes);
  detailsDiv.innerHTML = `
        <p><span class="label-bold">Запрос создал:</span> ${command.Created_By}</p>
        <p><span class="label-bold">Только скачать:</span> ${quicCommand.OnlyDownload ? 'Да' : 'Нет'}</p>
        <p><span class="label-bold">Удаление после установки:</span> ${quicCommand.NotDeleteAfterInstallation ? 'Не удалять' : 'Удалить'}</p>
        <p><span class="label-bold">С наивысшими правами:</span> ${quicCommand.RunWithHighestPrivileges ? 'Да' : 'Нет'}</p>
        <p><span class="label-bold">Выполнение:</span> ${quicCommand.RunWhetherUserIsLoggedOnOrNot ? 'Для всех пользователей (без доступа к GUI)' : 'Только для пользователей, вошедших в систему (с доспупом к GUI)'}</p>
        <p><span class="label-bold">Пользователь:</span> ${quicCommand.UserName || 'Не указано'}</p>
        <p><span class="label-bold">Аргументы:</span> ${quicCommand.ProgramRunArguments || 'Не указаны'}</p>
        <p><span class="label-bold">Хеш-сумма "XXH3":</span> ${quicCommand.XXH3}</p>
		<p><span class="label-bold">Размер файла:</span> ${fileSizeHuman}</p>
        <p><span class="label-bold">Путь к файлу:</span> ${quicCommand.DownloadRunPath.replace(/\\\\/g, '\\')}</p>
    `;
}

function displayInstallClients(clients) {
  const tbody = document.getElementById('installClientsTableBody');
  tbody.innerHTML = '';

  for (const [clientId, clientData] of Object.entries(clients)) {
    const row = document.createElement('tr');
    row.setAttribute('data-client-id', clientId);

    const statusHTML = buildInstallStatusHTML(clientData);
    row.innerHTML = `
      <td>${clientData.ClientName}</td>
      <td>${statusHTML}</td>
      <td>
        <button class="action-button delete tt-btn" data-client-id="${clientId}" data-tt="Удаление клиента из запроса на установку">✖</button>
        <button class="action-button restart tt-btn" data-client-id="${clientId}" data-tt="Повторная отправка запроса на установку">↻</button>
      </td>
    `;

    // Сохраняем сигнатуру в ячейке "Выполнено"
    const doneCell = row.cells[1];
    if (doneCell) {
      doneCell.setAttribute('data-sig', getInstallStatusSig(clientData));
    }

    tbody.appendChild(row);
  }

  tbody.querySelectorAll('.action-button').forEach(button => {
    button.addEventListener('click', function() {
      const clientId = this.dataset.clientId;
      const dateCreation = document.getElementById('installDateSelect').value;
      if (this.classList.contains('delete')) {
        sendActionRequest("/delete-client-QUIC-report", {
          client_id: clientId,
          Date_Of_Creation: dateCreation
        });
      } else if (this.classList.contains('restart')) {
        sendActionRequest("/resend-QUIC-report", {
          client_id: clientId,
          Date_Of_Creation: dateCreation
        });
      }
    });
  });
}

const InstallAutoUpdater = (function() {
  let timerId = null;

  function updateClientsColumn() {
    if (!document.getElementById("installReport").classList.contains("active")) {
      stop();
      return;
    }
    const selectedDate = document.getElementById("installDateSelect").value;
    if (!selectedDate) return;

    fetch('/get-QUIC-report', { method: 'GET', credentials: "same-origin" })
      .then((response) => response.json())
      .then((data) => {
        if (!data) return;
        const selectedCommand = data.find((item) => item.Date_Of_Creation === selectedDate);
        if (!selectedCommand) return;

        const clients = selectedCommand.ClientID_QUIC;
        Object.keys(clients).forEach((clientId) => {
          const clientData = clients[clientId];
          const row = document.querySelector(`#installClientsTableBody tr[data-client-id="${clientId}"]`);
          if (!row) return;

          const doneCell = row.cells[1];
          if (!doneCell) return;

          const newSig = getInstallStatusSig(clientData);
          const oldSig = doneCell.getAttribute('data-sig') || '';

          // Ничего не меняем, если данные идентичны — тултип не моргает
          if (newSig === oldSig) return;

          // Данные изменились — обновляем ячейку и сигнатуру
          doneCell.innerHTML = buildInstallStatusHTML(clientData);
          doneCell.setAttribute('data-sig', newSig);
        });
      })
      .catch((error) => console.error('Ошибка обновления колонки "Выполнено":', error));
  }

  function start() {
    if (timerId === null) {
      timerId = setInterval(updateClientsColumn, 5000);
    }
  }

  function stop() {
    if (timerId !== null) {
      clearInterval(timerId);
      timerId = null;
    }
  }
  return { start, stop };
})();



/* "По cmd / PowerShell" */

// Вызов загрузки дат
function loadCmdDates() {
    loadDates("/get-terminal-report", "cmdDateSelect", "cmd");
}

// Вызов удаления всего запроса с подтверждением
function deleteSelectedCommand() {
    deleteSelectedRequest("/delete-by-date-terminal-report", "cmdDateSelect", "cmd");
}

// Функция для загрузки отчёта по выбранной дате
function loadCmdReport() {
  const selectedDate = document.getElementById('cmdDateSelect').value;
  fetch('/get-terminal-report', { method: 'GET', credentials: "same-origin" })
  
    .then(response => response.json())
    .then(data => {
      if (!data || data.length === 0) {
        document.getElementById('cmdDetails').innerHTML = '';
        document.getElementById('clientsTableBody').innerHTML = '';
        // Если данных нет, обновляем выпадающий список
        document.getElementById('cmdDateSelect').innerHTML = '<option value="">Нет запросов</option>';
        showPush("Нет доступных запросов.", '#ff4081'); // Розовый
        return;
      }
      const selectedCommand = data.find(item => item.Date_Of_Creation === selectedDate);
      if (selectedCommand) {
        displayCmdDetails(selectedCommand);
        displayCmdClients(selectedCommand.ClientID_Command);
		
        const detailsDiv = document.getElementById('cmdDetails');
        const fieldset = document.querySelector('.cmd-fieldset-fixed');
		const savedState = sessionStorage.getItem('cmdDetailsState') || 'collapsed';
		
		if (savedState === 'expanded') {
		  fieldset.classList.add('expanded'); // <-- ограничиваем высоту + скролл
		  fieldset.classList.remove('collapsed');
		  document.getElementById('toggleCmdDetails').textContent = '-';
		} else {
		  fieldset.classList.add('collapsed');
		  fieldset.classList.remove('expanded');
		  document.getElementById('toggleCmdDetails').textContent = '+';
		}

        setClientsTableHeight();
		adjustHeaderPadding('cmd-clients-table-container');
      } else {
        // Если выбранного запроса нет, очистим детали и таблицу и обновим выпадающий список
        document.getElementById('cmdDetails').innerHTML = '';
        document.getElementById('clientsTableBody').innerHTML = '';
        loadCmdDates();
        showPush("Клиентов не осталось, запрос удалён", '#4CAF50'); // Зелёный
      }
    })
    .catch(error => {
      console.error('Ошибка загрузки отчёта:', error);
      showPush("Ошибка: " + error.message, '#ff4d4d'); // Красный 
    });
}

// Анимация и работа кнопки "+/-" для cmd/PowerShell
function toggleCmdDetails() {
  const fieldset = document.querySelector('.cmd-fieldset-fixed');
  const details = document.getElementById('cmdDetails');
  const toggleButton = document.getElementById('toggleCmdDetails');

  // Защита от повторного клика во время анимации
  if (details.dataset.animating === '1') return;
  details.dataset.animating = '1';

  const isExpanded = fieldset.classList.contains('expanded');

  if (isExpanded) {
    // Сворачивание — обе анимации синхронно
    sessionStorage.setItem('cmdDetailsState', 'collapsed');
    setClientsTableHeight();
    adjustHeaderPadding('cmd-clients-table-container');

    // Детали к 0px (плавное сворачивание)
    const startHeight = details.offsetHeight;
    details.style.maxHeight = startHeight + 'px';

    void details.getBoundingClientRect();
    details.style.maxHeight = '0px';
    toggleButton.textContent = '+';

    const onEnd = (e) => {
      if (e.propertyName !== 'max-height') return;
      details.removeEventListener('transitionend', onEnd);
	  
      // Только после окончания — переключаем класс "рамки"
      fieldset.classList.remove('expanded');
      fieldset.classList.add('collapsed');
	  
      // Чистим инлайн
      details.style.maxHeight = '';
	  
      // Финальная корректировка шапки (после анимации)
      setTimeout(() => adjustHeaderPadding('cmd-clients-table-container'), 0);
      details.dataset.animating = '0';
    };
    details.addEventListener('transitionend', onEnd);
  } else {
    // Разворачивание — обе анимации синхронно
    sessionStorage.setItem('cmdDetailsState', 'expanded');
    setClientsTableHeight();
    adjustHeaderPadding('cmd-clients-table-container');

    // Детали к целевому значению через CSS
    fieldset.classList.add('expanded');
    fieldset.classList.remove('collapsed');
    details.style.maxHeight = ''; // позволяем правилу .expanded #cmdDetails анимировать до 185px
    toggleButton.textContent = '-';

    const onEnd = (e) => {
      if (e.propertyName !== 'max-height') return;
      details.removeEventListener('transitionend', onEnd);
      setTimeout(() => adjustHeaderPadding('cmd-clients-table-container'), 0);
      details.dataset.animating = '0';
    };
    details.addEventListener('transitionend', onEnd);
  }
}

// Функция установки размера таблицы клиентов
function setClientsTableHeight() {
  const tableContainer = document.querySelector('.cmd-clients-table-container');
  if (!tableContainer) return;
  const state = sessionStorage.getItem('cmdDetailsState') || 'collapsed';
  if (state === 'expanded') {
    tableContainer.style.maxHeight = '294px';
  } else {
    tableContainer.style.maxHeight = '450px';
  }
}

// Привязка обработчиков событий
document.addEventListener('DOMContentLoaded', function() {
  document.getElementById('toggleCmdDetails').addEventListener('click', toggleCmdDetails);
  document.getElementById('toggleLegend').addEventListener('click', toggleCmdDetails);
  document.getElementById('deleteCommandBtn').addEventListener('click', deleteSelectedCommand);
  document.getElementById('clientsReportButton').addEventListener('click', openReportModal);
  document.getElementById('closeReportModal').addEventListener('click', closeReportModal);
  document.getElementById('toggleInstallDetails').addEventListener('click', toggleInstallDetails);
  document.getElementById('toggleLegendInstall').addEventListener('click', toggleInstallDetails);
  document.getElementById('deleteInstallBtn').addEventListener('click', deleteSelectedInstall);
  
  // Обработчики для кнопок вкладок
document.getElementById('installReportTab').addEventListener('click', function(event) {
    openReportTab(event, 'installReport');
});

document.getElementById('cmdReportTab').addEventListener('click', function(event) {
    openReportTab(event, 'cmdReport');
});

// Обработчики для select-элементов
document.getElementById('installDateSelect').addEventListener('change', loadInstallReport);
document.getElementById('cmdDateSelect').addEventListener('change', loadCmdReport);
});

// Функция для отображения деталей команды
function displayCmdDetails(command) {
  const detailsDiv = document.getElementById('cmdDetails');
  const teamCommand = JSON.parse(command.Team_Command);
  detailsDiv.innerHTML = `
    <p><span class="label-bold">Запрос создал:</span> ${command.Created_By}</p>
	<p><span class="label-bold">Терминал:</span> ${teamCommand.Terminal}</p>
    <p><span class="label-bold">С наивысшими правами:</span> ${teamCommand.RunWithHighestPrivileges ? 'Да' : 'Нет'}</p>
    <p><span class="label-bold">Выполнение:</span> ${teamCommand.RunWhetherUserIsLoggedOnOrNot ? 'Для всех пользователей (без доступа к GUI)' : 'Только для пользователей, вошедших в систему (с доспупом к GUI)'}</p>
    <p><span class="label-bold">Пользователь:</span> ${teamCommand.User || 'Не указано'}</p>
    <p><span class="label-bold">Рабочая папка:</span> ${teamCommand.WorkingFolder || 'Не указано'}</p>
    <p><span class="label-bold">Команда:</span></p>
  `;
  const commandTextEl = document.createElement('p');
  commandTextEl.textContent = teamCommand.Command;
  commandTextEl.style.whiteSpace = 'pre-wrap';
  detailsDiv.appendChild(commandTextEl);
}

// Функция для отображения списка клиентов
function displayCmdClients(clients) {
  const tbody = document.getElementById('clientsTableBody');
  tbody.innerHTML = '';
  for (const [clientId, clientData] of Object.entries(clients)) {
    const row = document.createElement('tr');
    // Добавляем data-атрибут для идентификации строки по clientId
    row.setAttribute('data-client-id', clientId);

    const answerText = clientData.Answer ?
	  `<span class="done">✓ - ` + clientData.Answer + `</span>` :
	  `<span class="pending">—</span>`;
    row.innerHTML = `
  <td>${clientData.ClientName}</td>
  <td>${answerText}</td>
  <td>
    <button class="action-button delete tt-btn" data-client-id="${clientId}" data-tt="Удаление клиента из запроса на выполнение">✖</button>
    <button class="action-button restart tt-btn" data-client-id="${clientId}" data-tt="Повторная отправка запроса на выполнение команды">↻</button>
  </td>
`;

    tbody.appendChild(row);
  }
  tbody.querySelectorAll('.action-button').forEach(button => {
    button.addEventListener('click', function() {
      const clientId = this.dataset.clientId;
      const dateCreation = document.getElementById('cmdDateSelect').value;
      if (this.classList.contains('delete')) {
        sendActionRequest("/delete-client-terminal-report", {
          client_id: clientId,
          Date_Of_Creation: dateCreation
        });
      } else if (this.classList.contains('restart')) {
        sendActionRequest("/resend-terminal-report", {
          client_id: clientId,
          Date_Of_Creation: dateCreation
        });
      }
    });
  });
}

// Всплывающие подсказки в модальном окне отчётов
(function() {
  // Переменные доступны только в этой области видимости
  const tooltipEl = document.getElementById('tooltipBox');
  let curTarget = null;

  // Обработчик наведения на элемент с классом .tt-btn
document.addEventListener('mouseover', e => {
  const target = e.target.closest('.tt-btn');
  if (!target) return;
  const text = target.getAttribute('data-tt');
  if (!text) return;

  clearTimeout(target._ttTimeout);

  target._ttTimeout = setTimeout(() => {
    const tipHTML = (text || '')
      .replace(/\r\n/g, '\n')
      .replace(/\n\n+/g, '<br><br>')  // абзац
      .replace(/\n/g, '<br>');        // перенос строки

    tooltipEl.innerHTML = tipHTML;

    // Ширина тултипа: для "Выполнено" элементы имеют класс tt-wide
    const isWide = target.classList.contains('tt-wide');
    tooltipEl.classList.toggle('wide', isWide);

    tooltipEl.style.display = 'block';

    // Позиционируем после установки контента и ширины
    const rect = target.getBoundingClientRect();
    tooltipEl.style.left = rect.left + (rect.width - tooltipEl.offsetWidth) / 2 + window.pageXOffset + 'px';
    tooltipEl.style.top  = rect.top  - tooltipEl.offsetHeight - 10 + window.pageYOffset + 'px';
    tooltipEl.style.opacity = '1';

    curTarget = target;
  }, 500);
});

  // Обработчик ухода курсора с элемента
  document.addEventListener('mouseout', (e) => {
    const target = e.target.closest('.tt-btn');
    if (!target) return;

    clearTimeout(target._ttTimeout);
    hideTooltip();
  });

function hideTooltip() {
  tooltipEl.style.opacity = '0';
  setTimeout(() => {
    tooltipEl.style.display = 'none';
    tooltipEl.innerHTML = '';
    tooltipEl.classList.remove('wide'); // сброс ширины, чтобы другие подсказки были 180px
    curTarget = null;
  }, 300);
}

  // MutationObserver – если элемент с активной подсказкой удаляется из DOM, принудительно скрываем подсказку
  const observer = new MutationObserver((mutations) => {
    for (const mutation of mutations) {
      for (const node of mutation.removedNodes) {
        if (node.contains && curTarget && node.contains(curTarget)) {
          hideTooltip();
        }
      }
    }
  });

  observer.observe(document.body, {
    childList: true,
    subtree: true
  });
})();

// Функция отправки запросов для операций удаления клиента или повторной отправки команды
function sendActionRequest(url, data) {
  apiPostJson(url, data)
  
    .then(response => response.json())
    .then(result => {
      if (result.status === 'Успех') {
        let pushColor = '#4CAF50'; // Зелёный по умолчанию
        if (url.includes('/resend-')) {
          pushColor = '#2196F3'; // Голубой для повторной отправки
        } else if (url.includes('/delete-client-')) {
          pushColor = '#4CAF50'; // Зелёный для удаления клиента
        }
        showPush(result.message, pushColor);
        if (url.includes('QUIC')) {
          loadInstallReport(); // Обновляем вкладку "По установкам ПО"
          setTimeout(() => {
            const tbody = document.getElementById('installClientsTableBody');
            if (tbody.children.length === 0) {
              loadInstallDates(); // Обновляем выпадающий список, если клиентов не осталось
            }
          }, 100);
        } else {
          loadCmdReport(); // Обновляем вкладку "По cmd / PowerShell"
          setTimeout(() => {
            const tbody = document.getElementById('clientsTableBody');
            if (tbody.children.length === 0) {
              loadCmdDates(); // Обновляем выпадающий список, если клиентов не осталось
            }
          }, 100);
        }
      } else {
        showPush(result.message, '#ff4081'); // Розовый
      }
    })
    .catch(error => {
      console.error('Ошибка:', error);
      showPush("Ошибка: " + error.message, '#ff4d4d'); // Красный
    });
}

// Функция для отображения кастомного модального окна подтверждения удаления запроса
function showConfirmModal(message, onConfirm) {
  const modal = document.getElementById("confirmDeleteRequestModal");
  const msgEl = document.getElementById("confirmMessage");
  const confirmButton = document.getElementById("confirmDeleteReportButton");
  const closeButton = document.getElementById("closeConfirmDeleteRequestModal");

  msgEl.textContent = message;
  modal.style.display = "flex";

  function confirmHandler() {
    onConfirm();
    modal.style.display = "none";
    cleanup();
  }

  function closeHandler() {
    modal.style.display = "none";
    cleanup();
  }

  function cleanup() {
    confirmButton.removeEventListener("click", confirmHandler);
    closeButton.removeEventListener("click", closeHandler);
  }

  confirmButton.addEventListener("click", confirmHandler);
  closeButton.addEventListener("click", closeHandler);

  // Устанавливаем фокус на кнопку "Удалить"
  confirmButton.focus();
}

const CmdAutoUpdater = (function() {
  // Приватная переменная для хранения ID таймера автообновления
  let timerId = null;

  // Приватная функция для обновления колонки "Выполнено"
  function updateClientsColumn() {
    // Если вкладка "По cmd / PowerShell" неактивна, останавливаем автообновление
    if (!document.getElementById("cmdReport").classList.contains("active")) {
      stop();
      return;
    }
    const selectedDate = document.getElementById("cmdDateSelect").value;
    if (!selectedDate) return;
	fetch('/get-terminal-report', { method: 'GET', credentials: "same-origin" })

      .then((response) => response.json())
      .then((data) => {
        if (!data) return;
        const selectedCommand = data.find((item) => item.Date_Of_Creation === selectedDate);
        if (!selectedCommand) return;
        const clients = selectedCommand.ClientID_Command;
        Object.keys(clients).forEach((clientId) => {
          const clientData = clients[clientId];
          // Поиск строки по data-атрибуту
          const row = document.querySelector(`#clientsTableBody tr[data-client-id="${clientId}"]`);
          if (row) {
            const doneCell = row.cells[1];
            if (doneCell) {
              doneCell.innerHTML = clientData.Answer ?
			  `<span class="done">✓ - ` + clientData.Answer + `</span>` :
			  `<span class="pending">—</span>`;
            }
          }
        });
      })
      .catch((error) => console.error('Ошибка обновления колонки "Выполнено":', error));
  }

  // Функция для запуска автообновления
  function start() {
    if (timerId === null) {
      timerId = setInterval(updateClientsColumn, 5000);
    }
  }

  // Функция для остановки автообновления
  function stop() {
    if (timerId !== null) {
      clearInterval(timerId);
      timerId = null;
    }
  }

  // Публичный API модуля
  return {
    start: start,
    stop: stop,
  };
})();



// МОДАЛЬНОЕ ОКНО "О проекте"

document.addEventListener("DOMContentLoaded", function() {
  // Получаем элементы один раз при загрузке
  const aboutProjectBtn = document.getElementById("aboutProject");
  const aboutModal = document.getElementById("aboutProjectModal");
  const aboutCloseBtn = document.getElementById("aboutCloseButton");
  const currentYearSpan = document.getElementById("currentYear");

  // Элементы OWASP CRS
  const elOwaspCurrent = document.getElementById("owaspCRSCurrentVersion");
  const elOwaspNew = document.getElementById("owaspCRSNewVersion");
  const btnOwaspUpdate = document.getElementById("owaspCRSUpdateBtn");
  const btnOwaspRollback = document.getElementById("owaspCRSRollbackBtn");
  const defaultOwaspRollbackTip = (btnOwaspRollback?.dataset?.tooltip);
  
  // Элементы FiReMQ
  const elFiCurrent = document.getElementById("firemqCurrentVersion");
  const elFiNew = document.getElementById("firemqNewVersion");
  const btnFiUpdate = document.getElementById("firemqUpdateBtn");
  const btnFiRollback = document.getElementById("firemqRollbackBtn");
  const defaultFiRollbackTip = (btnFiRollback?.dataset?.tooltip);

	// Если до перезагрузки был установлен reopenAbout — открываем модалку снова
    if (sessionStorage.getItem("reopenAbout") === "1") {
        sessionStorage.removeItem("reopenAbout");
        // Немного задержим, чтобы DOM точно успел построиться
        setTimeout(() => {
            showAboutModal();
        }, 200);
    }
	
  // Устанавливаем текущий год
  if (currentYearSpan) {
    currentYearSpan.textContent = new Date().getFullYear();
  }

	// Обновление OWASP CRS
  // Небольшой компаратор версий вида x.y.z (числовые части); если не удаётся — считаем равными
  function compareVersions(a, b) {
    if (!a || !b) return 0;
    const toNums = v => String(v).split(".").map(p => parseInt((p.match(/\d+/)?.[0]) ?? "0", 10));
    const A = toNums(a), B = toNums(b);
    const len = Math.max(A.length, B.length);
    for (let i = 0; i < len; i++) {
      const x = A[i] ?? 0, y = B[i] ?? 0;
      if (x > y) return 1;
      if (x < y) return -1;
    }
    return 0;
  }

  function setUpdateEnabled(enabled) {
    if (btnOwaspUpdate) btnOwaspUpdate.disabled = !enabled;
  }

  async function refreshOWASPCRSStatus() {
    if (!elOwaspCurrent || !elOwaspNew) return;
    elOwaspCurrent.textContent = "…";
    elOwaspNew.textContent = "…";
    setUpdateEnabled(false);

    try {
      const resp = await fetch("/check-OWASP-CRS", {
        method: "GET",
        headers: { "Accept": "application/json" },
        cache: "no-store",
      });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const data = await resp.json();

    const current = data?.CurrentVersion ?? "—";
    const latest = data?.NewVersion ?? "—";
    const backupVersion = data?.BackupVersion ?? null;

    elOwaspCurrent.textContent = current;
    elOwaspNew.textContent = latest;

    const canUpdate = compareVersions(latest, current) > 0;
    setUpdateEnabled(canUpdate);

    // Без PUSH, если бэкапа нет — просто дефолтный текст
    if (btnOwaspRollback) {
      if (typeof backupVersion === "string" && backupVersion.trim() !== "") {
        btnOwaspRollback.dataset.tooltip =
          `Откатывает правила к предыдущей (ранее установленной) версии ${backupVersion}? ` +
          `Это может понадобиться в случае серьёзных изменений новых правил, которые могут привести к некорректной работе FiReMQ.`;
      } else {
        btnOwaspRollback.dataset.tooltip = defaultOwaspRollbackTip;
      }
    }
  } catch (e) {
    elOwaspCurrent.textContent = "—";
    elOwaspNew.textContent = "—";
    setUpdateEnabled(false);
    if (btnOwaspRollback) btnOwaspRollback.dataset.tooltip = defaultOwaspRollbackTip;

    // PUSH только для сбоев запроса/сети/JSON, не связанных с отсутствия бэкапа
    showPush(`Не удалось проверить OWASP CRS: ${e.message}`, "#ff4d4d");
  }
}

  async function handleUpdateOWASP() {
    if (!btnOwaspUpdate || !btnOwaspRollback) return;
    btnOwaspUpdate.disabled = true;
    btnOwaspRollback.disabled = true;
    try {
		  const resp = await apiPostJson("/update-OWASP-CRS", {});
		  const data = await resp.json().catch(() => ({}));

		  if (!resp.ok) {
			throw new Error(data?.Description);
		  }

		  const answer = data?.UpdateAnswer;
		  if (answer === "Успех") {
			showPush(`Правила OWASP CRS обновлены до версии ${data?.Version ?? ""}`, "#4CAF50"); // Зелёный
		  } else if (answer === "Обновление не требуется") {
			showPush("Обновление не требуется.", "#ff4081"); // Розовый
		  } else {
			throw new Error(data?.Description);
		  }
    } catch (e) {
      showPush(e.message, "#ff4d4d"); // Красный
    } finally {
      await refreshOWASPCRSStatus();
      btnOwaspRollback.disabled = false; // состояние кнопки "Обновить" установит refresh
    }
  }

  async function handleRollbackOWASP() {
    if (!btnOwaspUpdate || !btnOwaspRollback) return;
    btnOwaspUpdate.disabled = true;
    btnOwaspRollback.disabled = true;
    try {
        const resp = await apiPostJson("/rollback-backup-OWASP-CRS", {});
        const data = await resp.json().catch(() => ({}));

        if (!resp.ok) {
            throw new Error(data?.Description);
        }

        if (data?.RollbackAnswer === "Успех") {
            showPush(`Откат выполнен. Текущая версия: ${data?.RollbackVersion ?? "неизвестно"}`, "#2196F3"); // Голубой
        } else if (data?.Description?.includes("Не требуется")) {
            showPush(data?.Description, "#ff4081"); // Розовый
        } else {
            throw new Error(data?.Description);
        }
    } catch (e) {
        showPush(e.message, "#ff4d4d"); // Красный
    } finally {
        await refreshOWASPCRSStatus();
        btnOwaspRollback.disabled = false; 
    }
  }


	// Обновление FiReMQ
	function setFiUpdateEnabled(enabled) {
	  if (btnFiUpdate) btnFiUpdate.disabled = !enabled;
	}

	async function refreshFiReMQStatus() {
	  if (!elFiCurrent || !elFiNew) return;
	  elFiCurrent.textContent = "…";
	  elFiNew.textContent = "…";
	  setFiUpdateEnabled(false);

	  try {
		const resp = await fetch("/check-FiReMQ", {
		  method: "GET",
		  headers: { "Accept": "application/json" },
		  cache: "no-store",
		});
		const data = await resp.json().catch(() => ({}));

		if (!resp.ok) {
		  // Если сервер всё же прислал JSON с текущей версией — покажем её
		  if (data && data.CurrentVersion) {
			elFiCurrent.textContent = data.CurrentVersion;
			elFiNew.textContent = "—";
		  }
		  const errMsg = data?.error || data?.Description || `HTTP ${resp.status}`;
		  throw new Error(errMsg);
		}

		// Служебный заголовок от сервера "X-FiReMQ-Repo-State", сообщает состояние релиза в репозитории относительно текущей версии:
		//   ok    — релиз равен текущей или новее
		//   older — релиз в репозитории старее
		//   none  — релизов или подходящих ассетов нет вовсе
		const repoState = resp.headers.get("X-FiReMQ-Repo-State");
		
		const current = data?.CurrentVersion ?? "—";
		const latest = data?.NewVersion ?? null;
		const backup = data?.BackupVersion ?? null;

		elFiCurrent.textContent = current;

		if (latest && String(latest).trim()) {
			// Пришла равная ил�� более новая версия
			elFiNew.textContent = latest;
			setFiUpdateEnabled(compareVersions(latest, current) > 0);
		} else {
			// Нет новой версии для показа (старее или релизов нет)
			elFiNew.textContent = "—";
			setFiUpdateEnabled(false);

			// Розовый PUSH — только когда репозиторий старее
			if (repoState === "older") {
				showPush(`В репозитории доступна более старая версия, чем установлена (${current}).`, "#ff4081");
			}
		}

    // Динамический tooltip с версией из бэкапа (если есть); иначе — дефолт
    if (btnFiRollback) {
      if (backup && String(backup).trim()) {
        btnFiRollback.dataset.tooltip =
          `Откатывает сервер FiReMQ к предыдущей (ранее установленной) версии ${backup}? ` +
          `Это может понадобиться в случае, если после обновления FiReMQ возникли проблемы.`;
      } else {
        btnFiRollback.dataset.tooltip = defaultFiRollbackTip;
      }
    }
  } catch (e) {
    elFiCurrent.textContent = "—";
    elFiNew.textContent = "—";
    setFiUpdateEnabled(false);
    if (btnFiRollback) btnFiRollback.dataset.tooltip = defaultFiRollbackTip;

    // Ошибка запроса/сети — показываем PUSH
    showPush(`Не удалось проверить FiReMQ: ${e.message}`, "#ff4d4d"); // Красный
  }
}

	async function handleUpdateFiReMQ() {
	  if (!btnFiUpdate || !btnFiRollback) return;
	  btnFiUpdate.disabled = true;
	  btnFiRollback.disabled = true;
	  try {
		const resp = await apiPostJson("/update-FiReMQ", {});
		const data = await resp.json().catch(() => ({}));

		if (resp.ok) {
		  // Успех: сервер запланирует shutdown через ~1 сек
		  const targetVer = data?.latest || data?.Version || elFiNew.textContent || "";
		  showPush(`FiReMQ обновляется до версии ${targetVer}. Сервер перезапустится автоматически.`, "#4CAF50"); // Зелёный
		  
		  // <-- Новое: автоперезагрузка страницы через 5,5 сек и повтор открытия About
		setTimeout(() => {
			sessionStorage.setItem("reopenAbout", "1");
			location.reload();
		}, 5500);
		} else if (data?.Description?.toLowerCase().includes("обновление не требуется")) {
			showPush("Обновление не требуется.", "#ff4081"); // Розовый
		} else {
			throw new Error(data?.Description);
		}
	  } catch (e) {
		showPush(e.message, "#ff4d4d"); // Красный
	  } finally {
		// Сервер может уже уйти на перезагрузку. Пробуем обновить статусы "по возможности".
		try { await refreshFiReMQStatus(); } catch {}
		btnFiRollback.disabled = false; // состояние "Обновить" установит refresh
	  }
	}

	async function handleRollbackFiReMQ() {
	  if (!btnFiUpdate || !btnFiRollback) return;
	  btnFiUpdate.disabled = true;
	  btnFiRollback.disabled = true;
	  try {
		const resp = await apiPostJson("/rollback-backup-FiReMQ", {});
		const data = await resp.json().catch(() => ({}));

		if (!resp.ok) {
		  throw new Error(data?.Description);
		}

		if (data?.ok === true || (data?.RollbackAnswer === "Успех")) {
		  // После ответа сервер завершится и ServerUpdater восстановит предыдущий релиз
		  showPush(data?.message, "#2196F3"); // Голубой
		  
		  // <-- Новое: автоперезагрузка страницы через 5,5 сек
		setTimeout(() => {
			sessionStorage.setItem("reopenAbout", "1");
			location.reload();
		}, 5500);
		} else if (data?.RollbackAnswer === "Не требуется") {
		// Просто информируем пользователя
		showPush(data?.message, "#ff4081"); // Розовый
		} else {
			throw new Error(data?.Description);
		}
	  } catch (e) {
		showPush(e.message, "#ff4d4d"); // Красный
	  } finally {
		try { await refreshFiReMQStatus(); } catch {}
		btnFiRollback.disabled = false; // состояние "Обновить" установит refresh
	  }
	}

  // Функция показа модального окна
  function showAboutModal() {
    aboutModal.style.display = "flex";
    aboutCloseBtn.focus(); // Устанавливаем фокус на кнопку "Закрыть"
    // При открытии сразу проверяем версии OWASP CRS
    refreshOWASPCRSStatus();
	refreshFiReMQStatus();
  }

	// Подписки на клики по кнопкам FiReMQ
	if (btnFiUpdate) {
	  btnFiUpdate.addEventListener("click", handleUpdateFiReMQ);
	}
	if (btnFiRollback) {
	  btnFiRollback.addEventListener("click", handleRollbackFiReMQ);
	}

  // Делаем функцию закрытия доступной глобально
  window.closeAboutModal = function() {
    aboutModal.style.display = "none";
  };

  // Обработчики событий
  if (aboutProjectBtn) {
    aboutProjectBtn.addEventListener("click", showAboutModal);
  }
  if (aboutCloseBtn) {
    // Закрытие через клик по кнопке
    aboutCloseBtn.addEventListener("click", closeAboutModal);
    // Закрытие по нажатию Enter
    aboutCloseBtn.addEventListener("keydown", function(event) {
      if (event.key === "Enter") {
        closeAboutModal();
      }
    });
  }

  if (btnOwaspUpdate) {
    btnOwaspUpdate.addEventListener("click", handleUpdateOWASP);
  }
  if (btnOwaspRollback) {
    btnOwaspRollback.addEventListener("click", handleRollbackOWASP);
  }
});



// МОДАЛЬНОЕ ОКНО УДАЛЕНИЯ FiReAgent

// Вспомогательный нормализатор статусов
function isSuccessStatus(status) {
  const s = (status || "").toLowerCase();
  return s === "успех" || s === "ok" || s === "success";
}

// Рендер чипа клиента
function createClientChip(id, { pending = false, removable = false, onRemove = null, removeTitle = null } = {}) {
  const chip = document.createElement("span");
  chip.className = "client-id-chip" + (pending ? " pending" : "");
  chip.setAttribute("data-id", id);

  const text = document.createElement("span");
  text.textContent = id;
  chip.appendChild(text);

  if (removable && typeof onRemove === "function") {
    const btn = document.createElement("button");
    btn.className = "chip-remove";
    btn.type = "button";
    const title = removeTitle || (pending ? "Отменить удаление" : "Убрать из отправки");
    btn.title = title;
    btn.setAttribute("aria-label", `${title}: ${id}`);
    btn.textContent = "×";
    btn.addEventListener("click", () => onRemove(id, btn, chip));
    chip.appendChild(btn);
  }

  return chip;
}

// Загрузка и отрисовка оффлайн-очереди
async function loadPendingUninstallList() {
  const pendingList = document.getElementById("pendingUninstallList");
  if (!pendingList) return;

  // Плейсхолдер загрузки
  pendingList.innerHTML = '<div class="list-empty">Загрузка...</div>';

  try {
    const resp = await fetch("/uninstall-pending", { method: "GET", credentials: "same-origin" });
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    const ids = await resp.json();

    pendingList.innerHTML = "";

    if (!Array.isArray(ids) || ids.length === 0) {
      pendingList.innerHTML = '<div class="list-empty">Пусто...</div>';
      return;
    }

    ids.forEach((id) => {
      const chip = createClientChip(id, {
        pending: true,
        removable: true,
        onRemove: cancelPendingUninstall,
      });
      pendingList.appendChild(chip);
    });
  } catch (err) {
    pendingList.innerHTML = `<div class="list-empty">Ошибка загрузки: ${err.message}</div>`;
  }
}

// Функция отмены удаления FiReAgent у офлайн клиента
async function cancelPendingUninstall(clientId, btnEl, chipEl) {
  try {
    btnEl.disabled = true;

    const resp = await apiPostJson("/uninstall-cancel", { client_id: clientId });
    const contentType = resp.headers.get("content-type") || "";
    let ok = resp.ok;
    let message = "";

    if (contentType.includes("application/json")) {
      const data = await resp.json();
      message = (data.message || "").trim();
      ok = ok && isSuccessStatus(data.status);
    } else {
      message = (await resp.text()).trim();
    }

    if (!message) message = ok ? "Удаление отменено" : `Ошибка ${resp.status}`;

    if (ok && chipEl && chipEl.parentNode) {
      chipEl.parentNode.removeChild(chipEl);
      const pendingList = document.getElementById("pendingUninstallList");
      if (pendingList && pendingList.children.length === 0) {
        pendingList.innerHTML = '<div class="list-empty">Пусто...</div>';
      }
    }

    // Цвет PUSH: голубой при успешной отмене, красный при ошибке
    if (typeof showPush === "function") {
      showPush(message, ok ? "#2196F3" : "#ff4d4d"); // Голубой при успехе; Красный при ошибке
    }
  } catch (err) {
    if (typeof showPush === "function") {
      showPush(`Ошибка: ${err.message}`, "#ff4d4d"); // Красный
    }
  } finally {
    if (btnEl) btnEl.disabled = false;
  }
}

// Удаление ID из нижнего списка + снятие галочки
function removeSelectedFromUninstallList(clientId, _btnEl, chipEl) {
  // Удаляем чип из модалки
  if (chipEl && chipEl.parentNode) {
    chipEl.parentNode.removeChild(chipEl);
  }

  // Снимаем чекбокс в DOM (если есть)
  const key = `checkbox_${clientId}`;
  const checkbox = document.getElementById(key);
  if (checkbox) checkbox.checked = false;

  // Обновляем состояние чекбоксов
  try {
    if (typeof checkboxStates !== "undefined") {
      delete checkboxStates[key];
      sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates));
    } else {
      const saved = JSON.parse(sessionStorage.getItem("checkboxStates") || "{}");
      delete saved[key];
      sessionStorage.setItem("checkboxStates", JSON.stringify(saved));
    }
  } catch (e) {
    // ignore
  }

  // Обновим кнопки действий
  if (typeof updateClientActionButtons === "function") {
    updateClientActionButtons();
  }

  // Если ничего не выбрано — блокируем "Удалить"
  const btn = document.getElementById("confirmUninstallButton");
  if (btn) {
    const state = (typeof checkboxStates !== "undefined")
      ? checkboxStates
      : (JSON.parse(sessionStorage.getItem("checkboxStates") || "{}"));
    const anyChecked = Object.keys(state).some((k) => state[k] === true);
    if (!anyChecked) btn.disabled = true;
  }
}

// Показ модального окна удаления FiReAgent
async function showUninstallFiReAgentModal() {
  // Берём состояние чекбоксов из глобальной переменной, либо из sessionStorage
  const state = (typeof checkboxStates !== "undefined")
    ? checkboxStates
    : (JSON.parse(sessionStorage.getItem("checkboxStates")) || {});

  const checkedClients = Object.keys(state)
    .filter((key) => state[key] === true)
    .map((key) => key.replace("checkbox_", ""));

  if (checkedClients.length === 0) {
	showPush("Нет выбранных клиентов для отправки команды удаления FiReAgent.", "#ff4d4d"); // Красный
    return;
  }

  const modal = document.getElementById("uninstallFiReAgentModal");
  const listBox = document.getElementById("uninstallClientsList");
  const pendingList = document.getElementById("pendingUninstallList");
  const input = document.getElementById("uninstallConfirmationInput");
  const btn = document.getElementById("confirmUninstallButton");

  // Сброс состояния
  input.value = "";
  btn.disabled = true;
  listBox.innerHTML = "";
  if (pendingList) {
    pendingList.innerHTML = '<div class="list-empty">Загрузка...</div>';
  }

// Заполняем чипы текущего выбора
checkedClients.forEach((id) => {
  const chip = createClientChip(id, {
    pending: false,
    removable: true,
    onRemove: removeSelectedFromUninstallList,
    removeTitle: "Убрать из отправки",
  });
  listBox.appendChild(chip);
});

  // Открываем модалку
  modal.style.display = "flex";
  setTimeout(() => input.focus(), 50);

  // Подгружаем оффлайн-очередь
  loadPendingUninstallList();
}

// Закрытие модального окна удаления FiReAgent
function closeUninstallFiReAgentModal() {
  const modal = document.getElementById("uninstallFiReAgentModal");
  if (modal) modal.style.display = "none";
}

// Активация кнопки по фразе "Удаляем!"
document.getElementById("uninstallConfirmationInput")?.addEventListener("input", function (e) {
  const btn = document.getElementById("confirmUninstallButton");
  const val = e.target.value.trim().toLowerCase();
  btn.disabled = val !== "удаляем!";
});

// Подтверждение и отправка запроса на удаление FiReAgent
async function confirmUninstallFiReAgent() {
  const checkedClients = Object.keys(checkboxStates)
    .filter((key) => checkboxStates[key] === true)
    .map((key) => key.replace("checkbox_", ""));

  if (checkedClients.length === 0) {
	showPush("Нет выбранных клиентов для отправки команды удаления FiReAgent.", "#ff4d4d"); // Красный
    return;
  }

  try {
    const response = await apiPostJson("/uninstall-fireagent", checkedClients);
    const contentType = response.headers.get("content-type") || "";
    let message = "";
    let category = "unknown";

    if (contentType.includes("application/json")) {
      const data = await response.json();
      message = (data.message || "").trim();
      category = statusCategory(data.status);
    } else {
      message = (await response.text()).trim();
    }

    if (!response.ok) {
      category = "error";
    }

    if (!message) {
      message = category === "error" ? `Ошибка ${response.status}` : "Готово";
    }

    // Чистим состояния чекбоксов для success и warning
    if (category !== "error") {
      checkedClients.forEach((clientId) => {
        const key = `checkbox_${clientId}`;
        delete checkboxStates[key];
        sessionStorage.removeItem(key);
      });
      sessionStorage.setItem("checkboxStates", JSON.stringify(checkboxStates));
    }

    // Цвет PUSH: зелёный — успех, розовый — предупреждение, красный — ошибка
    let pushColor = "#4CAF50"; // Зелёный
    if (category === "warning") pushColor = "#ff4081"; // Розовый
    if (category === "error") pushColor = "#ff4d4d";   // Красный

    sessionStorage.setItem("pushMessage", message);
    sessionStorage.setItem("pushColor", pushColor);

  } catch (err) {
    sessionStorage.setItem("pushMessage", `Ошибка: ${err.message}`);
    sessionStorage.setItem("pushColor", "#ff4d4d");	// Красный
  } finally {
    closeUninstallFiReAgentModal();
    location.reload();
  }
}

// Категории статуса
function statusCategory(status) {
  const s = (status || "").toLowerCase();
  if (["успех", "ok", "success"].includes(s)) return "success";
  if (["предупреждение", "warning"].includes(s)) return "warning";
  if (["ошибка", "error", "fail", "failed"].includes(s)) return "error";
  return "unknown";
}

// Привязки к элементам модального окна удаления FiReAgent
document.getElementById("closeUninstallFiReAgentModal")?.addEventListener("click", closeUninstallFiReAgentModal);
document.getElementById("confirmUninstallButton")?.addEventListener("click", confirmUninstallFiReAgent);

// Enter внутри модального окна — выполнить, если подтверждение корректно
document.getElementById("uninstallFiReAgentModal")?.addEventListener("keydown", function (event) {
  if (event.key === "Enter") {
    const input = document.getElementById("uninstallConfirmationInput");
    if (input.value.trim().toLowerCase() === "удаляем!") {
      document.getElementById("confirmUninstallButton").click();
    }
  }
});