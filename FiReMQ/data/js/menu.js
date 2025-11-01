// Вывод имени пользователя после успешной авторизации
function fetchAuthName() {
  fetch("/get-authname")
    .then((response) => response.json())
    .then((data) => {
      if (data.auth_name) {
        document.getElementById("auth-name").innerText = data.auth_name;
      }
    })
    .catch((error) => console.error("Ошибка при получении имени пользователя:", error));
}

// Открытие выпадающего меню
function toggleDropdown() {
  document.getElementById("myDropdown").classList.toggle("show");
}

// Закрытие выпадающего меню при нажатии вне его
window.onclick = function (event) {
  if (!event.target.matches(".dropbtn")) {
    const dropdowns = document.getElementsByClassName("dropdown-content");
    for (let i = 0; i < dropdowns.length; i++) {
      const openDropdown = dropdowns[i];
      if (openDropdown.classList.contains("show")) {
        openDropdown.classList.remove("show");
      }
    }
  }
};

// Привязка обработчика к кнопке меню
document.addEventListener("DOMContentLoaded", function() {
    const menuButton = document.getElementById("menuButton");
    if (menuButton) {
        menuButton.addEventListener("click", toggleDropdown);
    }
});

// Обработчик для кнопки выхода
document.addEventListener("DOMContentLoaded", function() {
  const logoutButton = document.getElementById("logoutButton");
  if (logoutButton) {
    logoutButton.addEventListener("click", function() {
      // Чистим состояние групп/подгрупп и разворотов текущей сессии
      sessionStorage.removeItem("ui.selectedGroup");
      sessionStorage.removeItem("ui.selectedSubgroup");
      sessionStorage.removeItem("ui.expandedGroups");

      window.location.href = "/logout";
    });
  }
});

// Кнопка 'Удаление "FiReAgent"'
document.addEventListener("DOMContentLoaded", function () {
  const removeFiReAgentItem = document.getElementById("removeFiReAgent");
  if (removeFiReAgentItem) {
    removeFiReAgentItem.addEventListener("click", function (e) {
      e.preventDefault();
      // На всякий случай проверим, что есть выделенные
      if (!hasCheckedClients()) return;
      if (!this.classList.contains("removeFiReAgent-disabled")) {
        // Функция в modal.js
        showUninstallFiReAgentModal();
      }
    });
  }
});



// КНОПКИ "Установка ПО" и "Выполнить cmd / PowerShell"

// Функция для обновления состояния кнопок "Установка ПО" и "Выполнить cmd / PowerShell"
function updateClientActionButtons() {
  const installButton = document.getElementById("installProgramButton");
  const executeButton = document.getElementById("executeCommandButton");
  const removeFiReAgent = document.getElementById("removeFiReAgent");
  const hasChecked = hasCheckedClients(); // Испольется функция из "clients.js"

  installButton.disabled = !hasChecked;
  executeButton.disabled = !hasChecked;

if (removeFiReAgent) {
    removeFiReAgent.classList.toggle("removeFiReAgent-disabled", !hasChecked);
  }
}

// Привязка обработчика "Выполнить cmd / PowerShell" к кнопкам для вызова модальных окон
document.getElementById("executeCommandButton").addEventListener("click", function () {
  if (!this.disabled) {
    runCommand(); // Функция из "modal.js"
  }
});

// Привязка обработчика "Установка ПО" к кнопкам для вызова модальных окон
document.getElementById("installProgramButton").addEventListener("click", function () {
  if (!this.disabled) {
    installProgram(); // Функция из "modal.js"
  }
});

// Инициализация состояния кнопки при загрузке страницы
document.addEventListener("DOMContentLoaded", function () {
  updateClientActionButtons(); // Устанавливаем начальное состояние
});

// КНОПКА ПРОКРУТКИ ВВЕРХ

// Создаём кнопку прокрутки вверх
const scrollToTopButton = document.createElement("button");
scrollToTopButton.className = "scroll-to-top";
scrollToTopButton.innerHTML = "↑";
document.body.appendChild(scrollToTopButton);

// Проверяем, нужно ли показать кнопку
function toggleScrollToTopButton() {
  if (window.scrollY > 100) {
    // Если прокрутка больше 100px
    scrollToTopButton.style.opacity = "1";
    scrollToTopButton.style.visibility = "visible";
    scrollToTopButton.style.transform = "translateY(0)"; // Плавно всплывает вверх
  } else {
    scrollToTopButton.style.opacity = "0";
    scrollToTopButton.style.visibility = "hidden";
    scrollToTopButton.style.transform = "translateY(100px)"; // Скрывается вниз
  }
}

// Добавляем обработчик прокрутки
window.addEventListener("scroll", toggleScrollToTopButton);

// Прокрутка страницы вверх при клике на кнопку
scrollToTopButton.addEventListener("click", () => {
  window.scrollTo({
    top: 0,
    behavior: "smooth",
  });
});

// ПРОВЕРКА СЕРВЕРА НА АВТОМАТИЧЕСКОЕ РАЗЛОГИРОВАНИЕ

// Функция для проверки статуса авторизации
function checkAuthStatus() {
  fetch("/check-auth", {
    method: "GET",
    credentials: "include", // Отправка куки
  })
    .then((response) => {
      if (response.status === 401) {
        // Если сервер вернул 401, перенаправляем на страницу авторизации
        window.location.href = "/auth.html";
      } else if (response.ok) {
        // Затем обновляем токен, если авторизация действительна
        refreshToken();
      }
    })
    .catch((error) => {
      console.error("Ошибка при проверке статуса авторизации:", error);
    });
}

// Функция для обновления токена "session_id"
function refreshToken() {
  fetch("/refresh-token", {
    method: "GET",
    credentials: "include", // Отправка куки
  }).catch((error) => {
    console.error("Ошибка при обновлении токена:", error);
  });
}

// Периодически проверяем статус авторизации каждые 10 секунд
setInterval(checkAuthStatus, 10000);
