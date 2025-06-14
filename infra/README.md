# infra build note

## set state backup

create bucket

```bash
gcloud storage buckets create gs://my-terraform-state-division-trip-money-20250614 --project=division-trip-money --location=ASIA-EAST1 --uniform-bucket-level-access
```

use bucket (my-terraform-state-division-trip-money-20250614) into terraform backend

## apply

deploy data layer

```bash
cd data
make deploy
```

deploy server layer

```bash
cd ../server
make deploy
```

Setting Project

- use `make secrets` can get secret inforamtion
- turn on WAF for SQL in cloud SQL (0.0.0.0/0)
- use `make remote-migration` to migrate DB with public IP and password
- turn off WAF for SQL in cloud SQL (0.0.0.0/0)
- set github action secret var in frontend and backend
  - `gh secret set GCP_PROJECT_ID`
    - `gcloud config get-value project`
  - `gh secret set REGISTER_NAME`
    - `gcloud artifacts repositories list`
  - `gh secret set GCP_SA_KEY`
    - download key from `https://console.cloud.google.com/iam-admin/serviceaccounts`
- trigger frontend and backend github action CD to push image
- set backend server url into frontend docker build image's ENV
  - `https://github.com/leon123858/dtmf/blob/main/dockerfile#L18`
- update ENV: `FRONTEND_URL` in backend env with cloud run
- set cloud run to new reversion

## destroy

```bash
cd server
make destroy
cd ../data
make destroy
```
