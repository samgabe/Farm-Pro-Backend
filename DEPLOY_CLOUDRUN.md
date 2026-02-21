# Deploy Backend to Cloud Run + Cloud SQL (PostgreSQL)

This is the working flow that succeeded for `farmpro-api` in `us-central1`.

## 1) Set variables

```bash
PROJECT_ID=galaxy-research-484209
REGION=us-central1
SERVICE=farmpro-api
DB_INSTANCE=farmpro-db
DB_NAME=farmpro
DB_USER=farmpro
CLOUDSQL_CONN="$PROJECT_ID:$REGION:$DB_INSTANCE"
```

## 2) Create Cloud SQL (if not already created)

```bash
gcloud sql instances create "$DB_INSTANCE" \
  --database-version=POSTGRES_14 \
  --tier=db-f1-micro \
  --region="$REGION" \
  --storage-type=SSD \
  --storage-size=10GB \
  --backup-start-time=03:00 \
  --enable-point-in-time-recovery \
  --maintenance-window-day=SUN \
  --maintenance-window-hour=04

gcloud sql databases create "$DB_NAME" --instance="$DB_INSTANCE"

gcloud sql users create "$DB_USER" \
  --instance="$DB_INSTANCE" \
  --password="newman"
```

## 3) Secrets (Secret Manager)

Create/update the required secrets:

```bash
printf 'host=/cloudsql/'"$CLOUDSQL_CONN"' user=farmpro password=newman dbname=farmpro sslmode=disable' | \
gcloud secrets create farmpro-database-url --data-file=- \
|| printf 'host=/cloudsql/'"$CLOUDSQL_CONN"' user=farmpro password=newman dbname=farmpro sslmode=disable' | \
gcloud secrets versions add farmpro-database-url --data-file=-

openssl rand -base64 32 | tr -d '\n' | \
gcloud secrets create farmpro-jwt-secret --data-file=- \
|| openssl rand -base64 32 | tr -d '\n' | \
gcloud secrets versions add farmpro-jwt-secret --data-file=-

printf '<smtp-app-password>' | \
gcloud secrets create SMTP_PASSWORD --data-file=- \
|| printf '<smtp-app-password>' | \
gcloud secrets versions add SMTP_PASSWORD --data-file=-
```

## 4) Build image (Cloud Build)

From repo root:

```bash
IMAGE="gcr.io/$PROJECT_ID/$SERVICE:manual-$(date +%Y%m%d-%H%M%S)"
gcloud builds submit ./backend --tag "$IMAGE"
```

## 5) Deploy Cloud Run service (single command)

Use an env file to avoid quoting issues:

```bash
cat > /tmp/run.env <<'EOF'
DB_SCHEMA_PATH=/app/db/schema.sql
FRONTEND_BASE_URL=https://farmpro-pi.vercel.app
CORS_ALLOWED_ORIGINS=https://farmpro-pi.vercel.app
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USERNAME=samgabe1998@gmail.com
FROM_EMAIL=noreply@farmpro.com
FROM_NAME=FarmPro
EOF

gcloud run deploy "$SERVICE" \
  --image "$IMAGE" \
  --region "$REGION" \
  --allow-unauthenticated \
  --service-account 18342609046-compute@developer.gserviceaccount.com \
  --add-cloudsql-instances "$CLOUDSQL_CONN" \
  --env-vars-file /tmp/run.env \
  --set-secrets DATABASE_URL=farmpro-database-url:latest,JWT_SECRET=farmpro-jwt-secret:latest,SMTP_PASSWORD=SMTP_PASSWORD:latest
```

## 6) Required IAM (once)

```bash
SA=$(gcloud run services describe "$SERVICE" \
  --region "$REGION" \
  --format="value(spec.template.spec.serviceAccountName)")

gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$SA" \
  --role="roles/cloudsql.client"

gcloud secrets add-iam-policy-binding farmpro-database-url \
  --member="serviceAccount:$SA" \
  --role="roles/secretmanager.secretAccessor"

gcloud secrets add-iam-policy-binding farmpro-jwt-secret \
  --member="serviceAccount:$SA" \
  --role="roles/secretmanager.secretAccessor"

gcloud secrets add-iam-policy-binding SMTP_PASSWORD \
  --member="serviceAccount:$SA" \
  --role="roles/secretmanager.secretAccessor"
```

## 7) Verify

```bash
URL=$(gcloud run services describe "$SERVICE" \
  --region "$REGION" \
  --format='value(status.url)')

curl -i "$URL/api/health"
```

Expected: `200` with JSON payload.

## 8) Frontend API base (Vercel)

Set:

```
VITE_API_BASE=https://farmpro-api-18342609046.us-central1.run.app
```

Redeploy the frontend.

# connection to cloudsql database 
cloud-sql-proxy --port 5433 galaxy-research-484209:us-central1:farmpro-db

psql "host=127.0.0.1 port=5433 dbname=postgres user=farmpro sslmode=disable"