# Интеграция компонентов

## Что было объединено

### 1. Backend от основной команды
- REST API с JWT авторизацией
- PostgreSQL с connection pooling
- Redis для кэширования
- Kafka для event-driven архитектуры
- Prometheus метрики
- Structured logging

### 2. Обработка файлов от друга
- Парсинг Excel (.xlsx, .xls) и CSV файлов
- Нормализация данных (поддержка русских и английских заголовков)
- Хеширование дипломов (SHA-256)
- Асинхронная обработка через worker pool
- Гибкая система маппинга колонок

### 3. Frontend
- SPA с 3 ролями (Студент, ВУЗ, Работодатель)
- Интеграция с backend API
- Drag & drop загрузка файлов
- Real-time статус обработки

## Архитектура интеграции

```
┌─────────────────┐
│   Frontend      │
│  (Vanilla JS)   │
└────────┬────────┘
         │ HTTP/REST
         ▼
┌─────────────────┐
│   API Server    │
│   (Gin/Go)      │
├─────────────────┤
│  - Auth         │
│  - Upload       │
│  - Verify       │
│  - CRUD         │
└────┬────────┬───┘
     │        │
     │        └──────────┐
     ▼                   ▼
┌─────────┐      ┌──────────────┐
│  Redis  │      │    Kafka     │
│ (Cache) │      │  (Events)    │
└─────────┘      └──────┬───────┘
                        │
                        ▼
                 ┌──────────────┐
                 │    Worker    │
                 │    Pool      │
                 ├──────────────┤
                 │ - Parse CSV  │
                 │ - Parse XLSX │
                 │ - Normalize  │
                 │ - Hash       │
                 │ - Insert DB  │
                 └──────┬───────┘
                        │
                        ▼
                 ┌──────────────┐
                 │  PostgreSQL  │
                 │  (Storage)   │
                 └──────────────┘
```

## Ключевые изменения

### 1. Worker Pool (`internal/worker/processor.go`)
- Интегрирован код парсинга от друга
- Добавлена поддержка Kafka events
- Асинхронная обработка через channels
- 3 concurrent workers

### 2. Upload Handler (`internal/api/handlers/diploma.go`)
- Сохранение файлов в `data/uploads/`
- Создание job в Redis
- Публикация события в Kafka
- Worker автоматически подхватывает задачу

### 3. Verify Handler
- Поиск по diploma_number, hash (QR), или ID
- Кэширование результатов в Redis
- Поддержка публичной верификации

### 4. Database Schema
- Добавлена таблица `upload_jobs`
- Поле `qr_code` в `diplomas` для хеша
- Индексы для быстрого поиска

### 5. Нормализация данных
Поддерживаемые варианты заголовков:
- **ФИО**: full_name, name, student, фио, студент
- **Номер**: diploma_number, number, diploma_no, номер
- **ВУЗ**: university, institution, вуз, университет
- **Специальность**: degree, qualification, specialty, степень
- **Дата**: date, issue_date, year, дата, год

## Workflow загрузки файла

1. **Frontend**: Пользователь загружает Excel/CSV
2. **API**: Сохраняет файл, создает job, публикует в Kafka
3. **Kafka**: Событие `file.uploaded`
4. **Worker**: Получает событие, парсит файл
5. **Worker**: Нормализует данные, генерирует хеши
6. **Worker**: Вставляет записи в PostgreSQL
7. **Worker**: Обновляет статус job
8. **Kafka**: Публикует `batch.processed`
9. **Frontend**: Получает обновление статуса

## Хеширование

```go
hash = SHA256(full_name + diploma_number + university + date + SECRET_KEY)
```

Хеш используется как:
- QR-код для диплома
- Уникальный идентификатор для верификации
- Защита от подделки

## API Endpoints (обновленные)

### Загрузка файла
```
POST /api/v1/university/upload
Content-Type: multipart/form-data

Response:
{
  "jobId": "uuid",
  "message": "File uploaded successfully"
}
```

### Статус обработки
```
GET /api/v1/university/queue

Response:
[
  {
    "jobId": "uuid",
    "filename": "diplomas.xlsx",
    "status": "processing",
    "progress": 45
  }
]
```

### Верификация (публичная)
```
GET /api/v1/verify/:id

Response:
{
  "valid": true,
  "name": "Иванов Иван Иванович",
  "university": "МГУ",
  "specialty": "Информатика",
  "year": "2023",
  "hash": "abc123..."
}
```

## Конфигурация

### Переменные окружения
```env
# Database
DATABASE_URL=postgres://user:password@localhost:5432/diasoft

# Redis
REDIS_URL=localhost:6379

# Kafka
KAFKA_BROKERS=localhost:9092
KAFKA_GROUP=diasoft-api

# Security
JWT_SECRET=your-secret-key
DIPLOMA_SECRET_KEY=your-diploma-hash-secret

# Server
SERVER_ADDRESS=:8080
ENVIRONMENT=production
```

## Запуск

```bash
# Full stack
docker-compose up

# С масштабированием
docker-compose up --scale api=3

# Development
go run cmd/api/main.go
```

## Тестирование

### Загрузка CSV
```bash
curl -X POST http://localhost:8080/api/v1/university/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@diplomas.csv"
```

### Проверка статуса
```bash
curl http://localhost:8080/api/v1/university/queue \
  -H "Authorization: Bearer $TOKEN"
```

### Верификация диплома
```bash
curl http://localhost:8080/api/v1/verify/abc123hash
```

## Производительность

- **Парсинг**: ~1000 записей/сек
- **Worker pool**: 3 concurrent workers
- **Batch insert**: Оптимизировано для PostgreSQL
- **Кэширование**: Redis TTL 30 мин для верификации

## Безопасность

✅ JWT авторизация для загрузки
✅ Rate limiting (100 req/min)
✅ Валидация типов файлов
✅ Хеширование с секретным ключом
✅ SQL injection protection (prepared statements)
✅ CORS настроен

## Мониторинг

- Prometheus metrics на `/metrics`
- Health check на `/health`
- Structured logs (JSON)
- Kafka consumer lag monitoring

## Что дальше

- [ ] WebSocket для real-time обновлений статуса
- [ ] Batch API для массовой верификации
- [ ] QR-код генерация на backend
- [ ] Email уведомления при завершении обработки
- [ ] Детальная статистика по загрузкам
