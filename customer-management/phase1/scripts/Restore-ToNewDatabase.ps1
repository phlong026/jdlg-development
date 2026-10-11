param(
 [Parameter(Mandatory=$true)][string]$BackupFile,
 [Parameter(Mandatory=$true)][ValidatePattern('^iyque_restore_[a-z0-9_]{1,40}$')][string]$TargetDatabase,
 [ValidateSet('compose.yml','compose.demo.yml')][string]$ComposeFile='compose.yml'
)
$ErrorActionPreference='Stop'
$crmRoot=Split-Path $PSScriptRoot -Parent
$crmCompose=Join-Path $crmRoot $ComposeFile
$crmBackup=(Resolve-Path -LiteralPath $BackupFile).Path
$crmExpected=(Get-Content -LiteralPath ($crmBackup+'.sha256') -Raw).Trim()
if((Get-FileHash -LiteralPath $crmBackup -Algorithm SHA256).Hash -ne $crmExpected){throw '备份校验失败'}
docker compose -f $crmCompose cp $crmBackup db:/tmp/iyque-phase1-restore.sql
if($LASTEXITCODE -ne 0){throw '备份文件复制失败'}
# Target names are restricted above. CREATE DATABASE fails if the target already exists.
docker compose -f $crmCompose exec -T -e "CRM_RESTORE_DB=$TargetDatabase" db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -e "CREATE DATABASE $CRM_RESTORE_DB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"'
if($LASTEXITCODE -ne 0){throw '创建恢复库失败；请使用未存在的新库名'}
docker compose -f $crmCompose exec -T -e "CRM_RESTORE_DB=$TargetDatabase" db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot --default-character-set=utf8mb4 "$CRM_RESTORE_DB" < /tmp/iyque-phase1-restore.sql'
if($LASTEXITCODE -ne 0){throw '恢复失败；原数据库未变更，请检查新恢复库'}
docker compose -f $crmCompose exec -T -e "CRM_RESTORE_DB=$TargetDatabase" db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot "$CRM_RESTORE_DB" -e "DELETE FROM crm_session; SELECT COUNT(*) AS customers FROM crm_customer; SELECT COUNT(*) AS followups FROM crm_followup;"'
if($LASTEXITCODE -ne 0){throw '恢复结果检查失败'}
Write-Output "已恢复到新库 $TargetDatabase，旧登录会话已清空。原应用仍使用原库；本脚本不切换应用。"
