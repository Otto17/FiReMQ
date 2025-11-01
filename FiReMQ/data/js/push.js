// PUSH-УВЕДОМЛЕНИЯ

// Модуль PUSH-УВЕДОМЛЕНИЙ
(function () {
    const LIFE_MS = 4200;	// Текущее время жизни PUSH
    const ENTER_MS = 600;	// Длительность "въезда" уведомления сверху
    const EXIT_MS = 750;	// Длительность "выезда" уведомления вверх

    // Множество активных уведомлений
    const activePushes = new Set();

    // Создаёт или возвращает контейнер для уведомлений
    function ensureContainer() {
        let c = document.getElementById('pushContainer');
        if (c) return c;

        // Совместимость со старым ID
        const old = document.getElementById('pushMessage');
        if (old) {
            old.id = 'pushContainer';
            old.className = 'push-container';
            old.textContent = '';
            return old;
        }

        // Создание нового контейнера
        c = document.createElement('div');
        c.id = 'pushContainer';
        c.className = 'push-container';
        c.setAttribute('aria-live', 'polite');
        c.setAttribute('aria-atomic', 'false');
        document.body.appendChild(c);
        return c;
    }

    // Запускает таймер жизни уведомления и анимацию прогресс-бара
    function startLife(wrapper, ms) {
        if (wrapper._state === 'closing') return;

        const progress = wrapper._progressEl;
        wrapper._lifeRemaining = ms;
        wrapper._lifeStartTs = performance.now();
        wrapper._state = 'running';

        // Сброс и запуск анимации прогресс-бара
        progress.style.animation = 'none';
        void progress.offsetWidth;
        progress.style.animation = `pushLife ${ms}ms linear forwards`;
        progress.style.animationPlayState = 'running';

        // Таймер закрытия
        if (wrapper._lifeTimerId) clearTimeout(wrapper._lifeTimerId);
        wrapper._lifeTimerId = setTimeout(() => closePush(wrapper), ms);
    }

    // Приостанавливает таймер жизни уведомления
    function pauseLife(wrapper) {
        if (wrapper._state !== 'running') return;

        const now = performance.now();
        const elapsed = now - (wrapper._lifeStartTs || now);
        const remaining = Math.max(0, (wrapper._lifeRemaining || LIFE_MS) - elapsed);

        wrapper._lifeRemaining = remaining;
        if (wrapper._lifeTimerId) {
            clearTimeout(wrapper._lifeTimerId);
            wrapper._lifeTimerId = null;
        }
        if (wrapper._progressEl) {
            wrapper._progressEl.style.animationPlayState = 'paused';
        }
        wrapper._state = 'paused';
    }

    // Возобновляет таймер жизни уведомления
    function resumeLife(wrapper) {
        if (wrapper._state !== 'paused') return;
        startLife(wrapper, wrapper._lifeRemaining || LIFE_MS);
    }

    // Запускает анимацию появления уведомления
    function startEnter(wrapper) {
        wrapper.classList.add('show');
        setTimeout(() => wrapper.classList.remove('just-added'), 0);

        if (wrapper._afterEnterTimer) clearTimeout(wrapper._afterEnterTimer);
        wrapper._afterEnterTimer = setTimeout(() => {
            wrapper._afterEnterTimer = null;
            if (document.visibilityState === 'visible') {
                if (wrapper._state === 'pending' || wrapper._state === 'waiting-visible') {
                    startLife(wrapper, LIFE_MS);
                }
            } else {
                wrapper._state = 'waiting-visible';
            }
        }, ENTER_MS);
    }

    // Закрывает уведомление с анимацией
    function closePush(wrapper) {
        if (!wrapper || wrapper._closing) return;
        wrapper._closing = true;
        wrapper._state = 'closing';

        // Очистка таймеров
        if (wrapper._lifeTimerId) clearTimeout(wrapper._lifeTimerId);
        wrapper._lifeTimerId = null;
        if (wrapper._afterEnterTimer) {
            clearTimeout(wrapper._afterEnterTimer);
            wrapper._afterEnterTimer = null;
        }

        // Анимация скрытия
        wrapper.classList.add('hide');
        wrapper.classList.remove('show');

        // Удаление после анимации
        const removeTimer = setTimeout(() => {
            activePushes.delete(wrapper);
            wrapper.remove();
        }, EXIT_MS + 40);

        wrapper.addEventListener('transitionend', function onEnd(e) {
            if (e.target !== wrapper) return;
            if (e.propertyName === 'max-height') {
                wrapper.removeEventListener('transitionend', onEnd);
                clearTimeout(removeTimer);
                activePushes.delete(wrapper);
                wrapper.remove();
            }
        });
    }

    // Обработчик изменения видимости вкладки
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'hidden') {
            activePushes.forEach(w => {
                if (w._state === 'running') pauseLife(w);
                else if (w._state === 'pending') w._state = 'waiting-visible';
            });
        } else {
            activePushes.forEach(w => {
                if (w._state === 'paused') {
                    resumeLife(w);
                } else if (w._state === 'waiting-visible' || w._state === 'pending') {
                    if (!w.classList.contains('show')) startEnter(w);
                    else startLife(w, LIFE_MS);
                }
            });
        }
    });

    // Публичная функция для показа уведомления
    window.showPush = function (message, backgroundColor = '#4CAF50') {
        const container = ensureContainer();
        const wrapper = document.createElement('div');
        wrapper.className = 'push-item just-added';
        wrapper._state = 'pending';
        activePushes.add(wrapper);

        // Карточка уведомления
        const card = document.createElement('div');
        card.className = 'push-card';
        card.style.setProperty('--bg', backgroundColor);

        // Текст уведомления
        const content = document.createElement('div');
        content.className = 'push-content';
        content.textContent = String(message);

        // Кнопка закрытия
        const closeBtn = document.createElement('button');
        closeBtn.className = 'push-close';
        closeBtn.type = 'button';
        closeBtn.setAttribute('aria-label', 'Закрыть уведомление');
        closeBtn.textContent = '×';
        closeBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            closePush(wrapper);
        });

        // Прогресс-бар
        const progress = document.createElement('div');
        progress.className = 'push-progress';
        wrapper._progressEl = progress;

        // Спейсер под карточкой
        const spacer = document.createElement('div');
        spacer.className = 'push-spacer';

        // Сборка DOM
        card.appendChild(closeBtn);
        card.appendChild(content);
        card.appendChild(progress);
        wrapper.appendChild(card);
        wrapper.appendChild(spacer);
        container.appendChild(wrapper);

        // Установка высоты обёртки
        const h = wrapper.scrollHeight;
        wrapper.style.setProperty('--target-height', h + 'px');

        // Запуск анимации в зависимости от видимости вкладки
        if (document.visibilityState === 'visible') {
            requestAnimationFrame(() => startEnter(wrapper));
        } else {
            wrapper._state = 'waiting-visible';
        }

        return wrapper;
    };
})();
