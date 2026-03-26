# InteractiveWhatsMeow

Serviço em Go com `whatsmeow`, API HTTP e UI estática para sessões WhatsApp multi-instância.

## Subir com Docker Compose

1. Copie as variáveis de ambiente de exemplo:

```bash
cp .env.example .env
```

2. Suba a stack local:

```bash
docker compose up --build
```

3. Acesse:

- API/UI: `http://localhost:3000`
- Health check: `http://localhost:3000/health`
- Dashboard: `http://localhost:3000/dashboard`

O serviço aplica as migrations e faz o bootstrap da `API_KEY` automaticamente no startup. Na UI, abra `http://localhost:3000/settings` e salve a mesma chave definida em `API_KEY` para autenticar as chamadas `/v1/...`.

## Variáveis principais

O `docker compose` lê automaticamente o arquivo `.env`.

- `API_KEY`: chave exigida pelo header `X-API-Key`
- `POSTGRES_DB`: nome do banco PostgreSQL
- `POSTGRES_USER`: usuário do banco
- `POSTGRES_PASSWORD`: senha do banco
- `APP_ENV`: ambiente lógico do app
- `WEBHOOK_BATCH_SIZE`: tamanho do lote do worker de webhooks
- `WEBHOOK_MAX_ATTEMPTS`: máximo de tentativas por entrega
- `WEBHOOK_POLL_INTERVAL`: intervalo de polling do worker
- `WEBHOOK_REQUEST_TIMEOUT`: timeout de cada request de webhook
- `PAIR_CODE_DISPLAY_NAME`: nome exibido no fluxo de pairing code

## Operação

Parar os containers:

```bash
docker compose down
```

Parar e limpar o ambiente local, incluindo o estado persistido do PostgreSQL:

```bash
docker compose down -v
```

O volume nomeado `postgres_data` preserva:

- estado do banco da aplicação
- sessões/dispositivos do `whatsmeow`
- histórico persistido de mensagens e webhooks

## Desenvolvimento sem Docker

Se preferir rodar localmente:

```bash
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/interactivewhatsmeow?sslmode=disable'
export API_KEY='dev-change-me'
export ADDR=':3000'

go run ./cmd/api
```
