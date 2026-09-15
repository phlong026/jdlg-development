#!/usr/bin/env bash
# Optional BaoTa launcher. Reads literal KEY=VALUE without eval or shell sourcing.
# Usage: bash start-crm.sh /absolute/jdk17/bin/java /absolute/app.jar /absolute/crm.env
set -euo pipefail
if [[ $# -ne 3 ]]; then
  printf '%s\n' 'Usage: start-crm.sh JAVA_EXECUTABLE APP_JAR ENV_FILE' >&2
  exit 2
fi
crm_java=$1
crm_jar=$2
crm_env=$3
[[ -x "$crm_java" && -r "$crm_jar" && -r "$crm_env" ]] || {
  printf '%s\n' 'Check Java executable, JAR and environment file permissions.' >&2
  exit 2
}
while IFS= read -r crm_line || [[ -n "$crm_line" ]]; do
  crm_line=${crm_line%$'\r'}
  [[ "$crm_line" =~ ^[[:space:]]*(#|$) ]] && continue
  [[ "$crm_line" == *=* ]] || { printf '%s\n' 'Invalid environment line.' >&2; exit 2; }
  crm_name=${crm_line%%=*}
  crm_value=${crm_line#*=}
  [[ "$crm_name" =~ ^(CRM_[A-Z0-9_]+|WECOM_[A-Z0-9_]+|SERVER_ADDRESS|SERVER_PORT)$ ]] || {
    printf '%s\n' 'Unexpected environment key.' >&2; exit 2;
  }
  [[ "$crm_value" != *REPLACE_* ]] || {
    printf 'Unfilled configuration: %s\n' "$crm_name" >&2; exit 2;
  }
  export "$crm_name=$crm_value"
done < "$crm_env"
exec "$crm_java" -Duser.timezone=Asia/Shanghai -jar "$crm_jar"
