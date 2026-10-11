param([ValidateSet('compose.yml','compose.demo.yml')][string]$ComposeFile='compose.yml')
$ErrorActionPreference='Stop'
$crmRoot=Split-Path $PSScriptRoot -Parent
$crmCompose=Join-Path $crmRoot $ComposeFile
$crmBackups=Join-Path $crmRoot 'backups'
New-Item -ItemType Directory -Force -Path $crmBackups | Out-Null
$crmName='crm-'+(Get-Date -Format 'yyyyMMdd-HHmmss')+'-'+[guid]::NewGuid().ToString('N').Substring(0,8)+'.sql'
$crmOutput=Join-Path $crmBackups $crmName
# Dump directly in the container so PowerShell never re-encodes SQL or prints credentials.
docker compose -f $crmCompose exec -T db sh -c 'umask 077; MYSQL_PWD="$MYSQL_PASSWORD" mysqldump -u"$MYSQL_USER" --single-transaction --no-tablespaces --set-gtid-purged=OFF --default-character-set=utf8mb4 "$MYSQL_DATABASE" > /tmp/iyque-phase1-backup.sql'
if($LASTEXITCODE -ne 0){throw '数据库备份失败'}
docker compose -f $crmCompose cp db:/tmp/iyque-phase1-backup.sql $crmOutput
if($LASTEXITCODE -ne 0){throw '备份文件复制失败'}
$crmDigest=(Get-FileHash -LiteralPath $crmOutput -Algorithm SHA256).Hash
[System.IO.File]::WriteAllText($crmOutput+'.sha256',$crmDigest,[System.Text.UTF8Encoding]::new($false))
Write-Output $crmOutput
