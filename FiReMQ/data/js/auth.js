document.addEventListener("DOMContentLoaded", async () => {
  const form = document.getElementById("authForm");
  const errorMessage = document.getElementById("error-message");
  const captchaContainer = document.getElementById("captcha-container");
  const captchaImage = document.getElementById("captcha-image");
  const captchaIdInput = document.getElementById("captcha_id");
  const captchaAnswerInput = document.getElementById("captcha_answer");

  // Функция для валидации ввода
  function validateAuth(input) {
    const regex = /^[a-zA-Z0-9а-яА-Я_!@#$%.?-]+$/;
    return regex.test(input);
  }

  // Проверяем, требуется ли капча для текущего IP
  try {
    const response = await fetch("/check-captcha", {
      method: "GET",
    });
    if (response.ok) {
      const data = await response.json();
      if (data.captcha_required) {
        // Загружаем капчу
        await fetchCaptcha();
      }
    }
  } catch (e) {
    console.error("Ошибка при проверке капчи:", e);
  }

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const auth_login = document.getElementById("auth_login").value.trim();
    const auth_password = document.getElementById("auth_password").value.trim();
    const captcha_answer = captchaAnswerInput.value.trim();
    const captcha_id = captchaIdInput.value.trim();

    // Проверка логина и пароля на соответствие разрешённым символам
    if (!validateAuth(auth_login)) {
      showError("Логин содержит запрещённые символы");
      return;
    }
    if (!validateAuth(auth_password)) {
      showError("Пароль содержит запрещённые символы");
      return;
    }

    const response = await fetch("/auth", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        auth_login,
        auth_password,
        captcha_id,
        captcha_answer,
      }),
    });

    if (response.ok) {
      // Успешная авторизация
      window.location.href = "/";
    } else {
      try {
        const errorData = await response.json();
        let errorMsg = errorData.error || "Ошибка авторизации";
        // Если требуется капча
        if (errorData.captcha_required) {
          // Показываем капчу
          await fetchCaptcha();
        }
        showError(errorMsg);
      } catch (e) {
        showError("Ошибка сервера");
      }
    }
  });

  async function fetchCaptcha() {
    try {
      const captchaResponse = await fetch("/captcha", {
        method: "GET",
      });
      if (captchaResponse.ok) {
        const captchaData = await captchaResponse.json();
        captchaImage.src = captchaData.image;
        captchaIdInput.value = captchaData.id;
        captchaContainer.className = "captcha-visible";
        captchaAnswerInput.required = true;
      } else {
        showError("Ошибка загрузки капчи");
      }
    } catch (e) {
      showError("Ошибка сети");
    }
  }

  function showError(message) {
    errorMessage.textContent = message;
    // Убедимся, что предыдущая анимация завершена
    errorMessage.classList.remove("visible");
    void errorMessage.offsetWidth; // Принудительное пересчитывание стилей
    // Запускаем анимацию появления
    errorMessage.classList.add("visible");
    setTimeout(() => {
      // Запускаем анимацию исчезновения
      errorMessage.classList.remove("visible");
    }, 3000); // Задержка перед исчезновением
  }
});
