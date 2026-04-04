# Diasoft Diploma Verification Platform

Production-ready fullstack платформа верификации дипломов с максимальной оптимизацией и масштабируемостью.

## Архитектура

### Backend (Go)
- **REST API** с JWT авторизацией
- **PostgreSQL** (pgx/v5) с connection pooling
- **Redis** для кэширования и rate limiting
- **Apache Kafka** для event-driven архитектуры
- **Worker Pool** для асинхронной обработки файлов
- **Prometheus** метрики
- **Structured logging** (Zap)

### Frontend (Vanilla JS)
- **SPA** с роутингом
- **3 роли**: Студент, ВУЗ, Работодатель
- **Drag & Drop** загрузка файлов
- **Real-time** статус обработки

### Обработка файлов
- **Excel** (.xlsx, .xls) и **CSV** парсинг
- **Нормализация** данных (русские/английские заголовки)
- **SHA-256** хеширование для QR-кодов
- **Асинхронная** обработка через Kafka

## Технологии
- **Backend**: Go 1.21, Gin, pgx/v5, Kafka, Redis
- **Frontend**: HTML5, CSS3, Vanilla JavaScript
- **Database**: PostgreSQL 15
- **Message Queue**: Apache Kafka
- **Cache**: Redis 7
- **File Processing**: excelize (Excel), csv (CSV)
- **Containerization**: Docker, Docker Compose
- **Orchestration**: Kubernetes (optional)

## Запуск

### Development
```bash
# Backend + Frontend + DB + Kafka + Redis
docker-compose up

# Только backend (нужен PostgreSQL, Redis, Kafka)
go run cmd/api/main.go
```

### Production
```bash
# С 3 репликами API
docker-compose up --scale api=3

# Или через Makefile
make docker-scale
```

Приложение доступно на `http://localhost:8080`

## API Endpoints

### Публичные
- `GET /` - Frontend приложение
- `GET /static/*` - Статические файлы
- `GET /health` - Health check
- `GET /metrics` - Prometheus метрики
- `GET /api/v1/verify/:id` - Публичная проверка диплома (по номеру, hash, или ID)

### Авторизация
- `POST /api/v1/auth/register` - Регистрация
- `POST /api/v1/auth/login` - Вход
- `POST /api/v1/auth/refresh` - Обновление токена

### Студент
- `GET /api/v1/student/profile` - Профиль студента
- `GET /api/v1/student/documents` - Список документов

### ВУЗ
- `POST /api/v1/university/upload` - Загрузка Excel/CSV (multipart/form-data)
- `GET /api/v1/university/records` - Реестр записей
- `GET /api/v1/university/queue` - Статус обработки файлов

### Работодатель
- `GET /api/v1/employer/history` - История проверок

### Дипломы (защищенные)
- `POST /api/v1/diplomas` - Создать диплом
- `GET /api/v1/diplomas/:id` - Получить диплом
- `GET /api/v1/diplomas` - Список с пагинацией
- `PUT /api/v1/diplomas/:id/verify` - Верифицировать

## Структура проекта
```
.
├── cmd/
│   └── api/
│       └── main.go              # Entry point
├── internal/
│   ├── api/
│   │   ├── handlers/            # HTTP handlers
│   │   ├── middleware/          # Middleware (auth, rate limit, CORS)
│   │   └── server.go            # Server setup
│   ├── config/                  # Configuration
│   ├── database/                # Database connection & migrations
│   ├── kafka/                   # Kafka producer/consumer
│   ├── logger/                  # Structured logging
│   ├── redis/                   # Redis client
│   └── worker/                  # File processing worker pool
├── web/
│   └── static/
│       ├── css/
│       │   └── style.css        # Styles
│       ├── js/
│       │   └── app.js           # Frontend logic
│       └── index.html           # SPA entry
├── data/
│   └── uploads/                 # Uploaded files storage
├── docker-compose.yml           # Docker orchestration
├── Dockerfile                   # Multi-stage build
├── k8s-deployment.yaml          # Kubernetes manifest
├── Makefile                     # Build commands
├── INTEGRATION.md               # Детальная документация интеграции
└── README.md
```

## Workflow загрузки файла

1. **Frontend**: Пользователь загружает Excel/CSV через drag & drop
2. **API**: Сохраняет файл в `data/uploads/`, создает job в Redis
3. **Kafka**: Публикует событие `file.uploaded`
4. **Worker**: Получает событие из Kafka, парсит файл
5. **Worker**: Нормализует данные, генерирует SHA-256 хеши
6. **Worker**: Batch insert в PostgreSQL
7. **Worker**: Обновляет статус job в Redis
8. **Kafka**: Публикует событие `batch.processed`
9. **Frontend**: Получает обновление статуса через polling

## Нормализация данных

Поддерживаемые варианты заголовков столбцов:

| Поле | Варианты |
|------|----------|
| **ФИО** | full_name, name, student, фио, студент, ФИО |
| **Номер диплома** | diploma_number, number, diploma_no, номер, номер диплома |
| **ВУЗ** | university, institution, вуз, университет, ВУЗ |
| **Специальность** | degree, qualification, specialty, степень, квалификация, специальность |
| **Дата/Год** | date, issue_date, year, дата, дата выдачи, год |

## Хеширование и QR-коды

```go
hash = SHA256(full_name + diploma_number + university + date + SECRET_KEY)
```

Хеш используется как:
- Уникальный QR-код для диплома
- Идентификатор для публичной верификации
- Защита от подделки документов

## Оптимизации

### Backend
✅ Connection pooling (50 max, 10 min)
✅ Database indexes на всех критичных полях
✅ Redis caching с автоинвалидацией (30 мин TTL)
✅ Rate limiting (100 req/min per IP)
✅ Graceful shutdown
✅ Request/response timeouts
✅ CORS middleware
✅ Structured logging (JSON)
✅ Health checks & metrics

### File Processing
✅ Worker pool (3 concurrent workers)
✅ Асинхронная обработка через Kafka
✅ Batch insert в PostgreSQL
✅ Гибкая нормализация данных
✅ Поддержка CSV (comma/semicolon) и Excel
✅ ~1000 записей/сек

### Frontend
✅ Минимальные зависимости (Vanilla JS)
✅ Client-side routing
✅ Token refresh mechanism
✅ Error handling
✅ Responsive design
✅ Drag & drop upload

### Infrastructure
✅ Docker multi-stage build
✅ Horizontal scaling (3+ replicas)
✅ Kafka для асинхронной обработки
✅ PostgreSQL оптимизация
✅ Redis LRU eviction
✅ Kubernetes HPA (3-10 pods)

## Переменные окружения
```env
# Server
SERVER_ADDRESS=:8080
ENVIRONMENT=production

# Database
DATABASE_URL=postgres://user:password@localhost:5432/diasoft?sslmode=disable

# Redis
REDIS_URL=localhost:6379

# Kafka
KAFKA_BROKERS=localhost:9092
KAFKA_GROUP=diasoft-api

# Security
JWT_SECRET=your-secret-key-change-in-production
DIPLOMA_SECRET_KEY=your-diploma-hash-secret
```

## Масштабирование

### Горизонтальное
- Stateless API - легко масштабируется
- Load balancer перед репликами
- Kafka consumers отдельно
- Worker pool независимый

### Вертикальное
- PostgreSQL connection pooling
- Redis memory optimization
- Kafka partitioning
- Worker pool size настраивается

## Kafka Events
- `diploma.created` - Диплом создан
- `diploma.verified` - Диплом верифицирован
- `file.uploaded` - Файл загружен (триггер для worker)
- `batch.processed` - Batch обработан

## Мониторинг
- Prometheus metrics на `/metrics`
- Health check на `/health`
- Structured logs (JSON)
- Kafka consumer lag monitoring
- Worker queue depth

## Безопасность
✅ JWT авторизация (15 мин access, 7 дней refresh)
✅ Bcrypt для паролей
✅ Rate limiting
✅ CORS настроен
✅ SQL injection protection (prepared statements)
✅ File type validation
✅ SHA-256 хеширование с секретным ключом

## Production Checklist
✅ Connection pooling
✅ Database indexes
✅ Redis caching
✅ Rate limiting
✅ Structured logging
✅ Graceful shutdown
✅ Health checks
✅ Metrics (Prometheus)
✅ CORS
✅ JWT + Refresh tokens
✅ Event-driven (Kafka)
✅ Docker multi-stage build
✅ Resource limits
✅ Frontend integration
✅ Static file serving
✅ API versioning
✅ File processing worker
✅ Excel/CSV parsing
✅ Data normalization
✅ QR code hashing

## Тестирование

### Загрузка файла
```bash
curl -X POST http://localhost:8080/api/v1/university/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@diplomas.xlsx"
```

### Проверка статуса
```bash
curl http://localhost:8080/api/v1/university/queue \
  -H "Authorization: Bearer $TOKEN"
```

### Верификация диплома
```bash
# По хешу
curl http://localhost:8080/api/v1/verify/abc123hash

# По номеру диплома
curl http://localhost:8080/api/v1/verify/ДП-2023-001
```

## Документация

- `README.md` - Основная документация
- `INTEGRATION.md` - Детальная документация интеграции компонентов
- `API.md` - API спецификация (TODO)

## Что дальше

- [ ] WebSocket для real-time обновлений
- [ ] Batch API для массовой верификации
- [ ] QR-код генерация (изображение)
- [ ] Email уведомления
- [ ] Детальная статистика
- [ ] Export в PDF
- [ ] Blockchain integration (опционально)

