# DiplomaVerify

Платформа верификации дипломов для ВУЗов, студентов и работодателей.

## Стек

**Бэкенд**
- Go 1.21, Gin
- PostgreSQL 15
- Redis 7
- Apache Kafka
- JWT (HS256), bcrypt

**Фронтенд**
- Vanilla JS (без фреймворков)
- HTML5 / CSS3
- WebSocket (real-time уведомления)
- BarcodeDetector API (сканирование QR)

**Инфраструктура**
- Docker / Docker Compose
- Prometheus metrics (`/metrics`)

---

## Роли

| Роль | Возможности |
|------|-------------|
| `student` | Просмотр диплома, привязка к реестру, генерация QR-ссылки |
| `university` | Загрузка Excel/CSV, ручное создание дипломов, статистика, экспорт CSV |
| `hr` (работодатель) | Проверка дипломов по ID/QR, пакетная проверка, история |
| `admin` | Модерация заявок ВУЗов, статистика платформы, управление пользователями |

---

## Быстрый старт

```bash
cp .env.example .env
# Заполни JWT_SECRET, ADMIN_EMAIL, ADMIN_PASSWORD в .env

docker compose up --build
```

Приложение доступно на `http://localhost:8080`

---

## Переменные окружения

| Переменная | Описание | Обязательна |
|-----------|----------|-------------|
| `JWT_SECRET` | Секрет для подписи токенов | ✅ |
| `DATABASE_URL` | PostgreSQL connection string | ✅ |
| `REDIS_URL` | Redis адрес | ✅ |
| `KAFKA_BROKERS` | Kafka брокеры через запятую | ✅ |
| `ADMIN_EMAIL` | Email администратора | — |
| `ADMIN_PASSWORD` | Пароль администратора | — |
| `PUBLIC_BASE_URL` | Публичный URL сервера (для QR-ссылок) | — |
| `ALLOWED_ORIGINS` | CORS origins через запятую | — |
| `DIPLOMA_SECRET_KEY` | Ключ для хеширования QR-кодов | — |

---

## Структура проекта

```
diplom/
├── cmd/api/main.go              # Точка входа
├── internal/
│   ├── api/
│   │   ├── handlers/            # HTTP хендлеры
│   │   ├── middleware/          # Auth, CORS, RateLimit, Security
│   │   └── server.go            # Роуты
│   ├── config/                  # Конфигурация из env
│   ├── database/                # Подключение, миграции
│   ├── kafka/                   # Producer / Consumer
│   ├── redis/                   # Подключение
│   ├── smtp/                    # SMTP сервис (опционально)
│   ├── worker/                  # Обработка Excel/CSV файлов
│   └── studentverify/           # Автосверка студента с реестром
└── web/site/                    # Фронтенд (HTML, CSS, JS)
```

---

## API

Все защищённые эндпоинты требуют заголовок `Authorization: Bearer <token>`.

**Auth**
- `POST /api/v1/auth/register` — регистрация
- `POST /api/v1/auth/login` — вход
- `POST /api/v1/auth/refresh` — обновление токена

**ВУЗ**
- `POST /api/v1/university/upload` — загрузка Excel/CSV
- `POST /api/v1/university/diplomas` — ручное создание диплома
- `GET /api/v1/university/records` — реестр с пагинацией
- `GET /api/v1/university/records/export` — экспорт CSV
- `GET /api/v1/university/stats` — статистика

**Верификация**
- `GET /api/v1/verify/:id` — публичная проверка
- `POST /api/v1/verify/batch` — пакетная проверка
- `GET /d/:token` — публичная страница диплома по QR-ссылке

**Работодатель**
- `GET /api/v1/employer/history` — история проверок
- `GET /api/v1/employer/history/export` — экспорт CSV

**Администратор**
- `GET /api/v1/admin/stats` — статистика платформы
- `GET /api/v1/admin/university-applications` — заявки ВУЗов
- `POST /api/v1/admin/university-applications/:id/approve` — одобрить
- `POST /api/v1/admin/university-applications/:id/reject` — отклонить
