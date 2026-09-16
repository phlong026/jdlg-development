#!/bin/sh
# Run only inside the named jd-crm-integration-db-1 container, using public demo databases.
set -eu
test "$MYSQL_DATABASE" = iyque_phase1
export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
mysql -uroot -e 'CREATE DATABASE jd_crm_restore_20260912; CREATE DATABASE jd_crm_upgrade_20260912; CREATE DATABASE jd_crm_management_restore_20260912;'
mysqldump -uroot --single-transaction --no-tablespaces --set-gtid-purged=OFF iyque_phase1 > /tmp/jd-crm-restore.sql
mysql -uroot jd_crm_restore_20260912 < /tmp/jd-crm-restore.sql
original=$(mysql -uroot -N -e 'SELECT CONCAT((SELECT COUNT(*) FROM iyque_phase1.crm_customer),":",(SELECT COUNT(*) FROM iyque_phase1.crm_followup),":",(SELECT COUNT(*) FROM iyque_phase1.crm_account));')
restored=$(mysql -uroot -N -e 'SELECT CONCAT((SELECT COUNT(*) FROM jd_crm_restore_20260912.crm_customer),":",(SELECT COUNT(*) FROM jd_crm_restore_20260912.crm_followup),":",(SELECT COUNT(*) FROM jd_crm_restore_20260912.crm_account));')
test "$original" = "$restored"
echo "PASS isolated backup/restore customer:followup:account counts $restored"
mysqldump -uroot --single-transaction --no-tablespaces --set-gtid-purged=OFF jd_crm_integration > /tmp/jd-crm-management-restore.sql
mysql -uroot jd_crm_management_restore_20260912 < /tmp/jd-crm-management-restore.sql
mysql -uroot -e 'SELECT user_id,scope,enabled,version FROM jd_crm_management_restore_20260912.management_access ORDER BY user_id;'
mysql -uroot jd_crm_upgrade_20260912 < /tmp/crm-v1.sql
mysql -uroot jd_crm_upgrade_20260912 -e "INSERT INTO crm_customer(id,name,version) VALUES ('legacy','retained',3); INSERT INTO crm_followup(customerId,operatorId,content) VALUES ('legacy','legacy-admin','retained history'); INSERT INTO crm_account(username,enabled) VALUES ('legacy-admin',TRUE);"
mysql -uroot jd_crm_upgrade_20260912 < /tmp/crm-v2.sql
upgraded=$(mysql -uroot -N jd_crm_upgrade_20260912 -e "SELECT CONCAT(c.name,':',c.version,':',c.addedTimeSource,':',a.authMode,':',(SELECT COUNT(*) FROM crm_followup)) FROM crm_customer c,crm_account a;")
test "$upgraded" = 'retained:3:unknown:standalone:1'
echo 'PASS populated V1 -> V2 SQL migration preserves customer, history, version and standalone account'
mysql -uroot -e "UPDATE iyque_phase1.crm_customer SET addedTimeSource='relation' WHERE id LIKE 'demo-customer-%' AND id<>'demo-customer-8'; UPDATE iyque_phase1.crm_customer SET stage='new',nextFollowupAt=DATE_SUB(NOW(6),INTERVAL 2 HOUR),version=version+1 WHERE id='demo-customer-1';"
echo 'Demo fixtures: seven known timestamps, one unknown, one overdue customer; history retained.'
