-- Prepared read-only audit; NEVER executed against production by this task.
-- Requires MySQL 8+ and users/user_profiles/user_identities schema.
-- Output is counts and account creation date only. No user IDs or raw phone/openid.
-- Nickname categories are exclusive and sum to user_total.
-- Credential columns overlap: one account may have WeChat + phone + password.
-- Stored nonempty OpenID is NOT proof this task verified it with WeChat.
-- 32hex is only the legacy migration shape test, not conclusive historical provenance.
-- Daily cohorts show CURRENT profile/credential state grouped by account creation date.
-- Empty datasets return zero totals; daily query returns no rows.
START TRANSACTION READ ONLY;

WITH account_snapshot AS (
  SELECT DATE(u.created_at) AS account_created_date,
    CASE
      WHEN p.user_id IS NULL THEN 'missing_profile'
      WHEN p.nickname IS NULL THEN 'nickname_null'
      WHEN CHAR_LENGTH(p.nickname) = 0 THEN 'nickname_empty'
      WHEN REGEXP_LIKE(p.nickname, '^[[:space:]]*$') THEN 'nickname_whitespace'
      ELSE 'nickname_valid'
    END AS nickname_state,
    (NULLIF(TRIM(COALESCE(u.openid, '')), '') IS NOT NULL) AS has_wechat_openid_value,
    (NULLIF(TRIM(COALESCE(u.unionid, '')), '') IS NOT NULL) AS has_unionid_value,
    REGEXP_LIKE(COALESCE(u.openid, ''), '^[0-9a-f]{32}$', 'c') AS has_legacy_32hex_openid_shape,
    EXISTS (
      SELECT 1 FROM user_identities i
      WHERE i.user_id = u.id AND i.identity_type = 'phone'
        AND NULLIF(TRIM(i.identity_key), '') IS NOT NULL
        AND i.verified_at IS NOT NULL
    ) AS has_verified_phone_login,
    EXISTS (
      SELECT 1 FROM user_identities i
      WHERE i.user_id = u.id AND i.identity_type = 'phone'
        AND NULLIF(TRIM(i.identity_key), '') IS NOT NULL
        AND i.verified_at IS NULL
    ) AS has_unverified_phone_identity,
    (NULLIF(TRIM(COALESCE(u.password, '')), '') IS NOT NULL) AS has_password_credential,
    (NULLIF(TRIM(COALESCE(p.phone, '')), '') IS NOT NULL) AS has_profile_phone_value,
    (COALESCE(p.enterprise_wechat_verified, 0) = 1
      AND NULLIF(TRIM(COALESCE(p.enterprise_wechat_contact, '')), '') IS NOT NULL
    ) AS has_enterprise_verified_contact
  FROM users u
  LEFT JOIN user_profiles p ON p.user_id = u.id
  WHERE u.user_type = 'miniprogram'
)
SELECT
  COUNT(*) AS user_total,
  COALESCE(SUM(nickname_state = 'missing_profile'), 0) AS profile_missing,
  COALESCE(SUM(nickname_state = 'nickname_null'), 0) AS nickname_null,
  COALESCE(SUM(nickname_state = 'nickname_empty'), 0) AS nickname_empty,
  COALESCE(SUM(nickname_state = 'nickname_whitespace'), 0) AS nickname_whitespace,
  COALESCE(SUM(nickname_state = 'nickname_valid'), 0) AS nickname_valid,
  COALESCE(SUM(has_wechat_openid_value), 0) AS with_wechat_openid_value,
  COALESCE(SUM(has_unionid_value), 0) AS with_unionid_value,
  COALESCE(SUM(has_legacy_32hex_openid_shape), 0) AS with_legacy_32hex_openid_shape,
  COALESCE(SUM(has_verified_phone_login), 0) AS with_verified_phone_login,
  COALESCE(SUM(has_unverified_phone_identity), 0) AS with_unverified_phone_identity,
  COALESCE(SUM(has_password_credential), 0) AS with_password_credential,
  COALESCE(SUM(has_profile_phone_value AND NOT has_verified_phone_login), 0) AS profile_phone_without_verified_login,
  COALESCE(SUM(has_enterprise_verified_contact AND NOT has_verified_phone_login), 0) AS enterprise_contact_without_verified_login
FROM account_snapshot;

WITH account_snapshot AS (
  SELECT DATE(u.created_at) AS account_created_date,
    CASE
      WHEN p.user_id IS NULL THEN 'missing_profile'
      WHEN p.nickname IS NULL THEN 'nickname_null'
      WHEN CHAR_LENGTH(p.nickname) = 0 THEN 'nickname_empty'
      WHEN REGEXP_LIKE(p.nickname, '^[[:space:]]*$') THEN 'nickname_whitespace'
      ELSE 'nickname_valid'
    END AS nickname_state,
    (NULLIF(TRIM(COALESCE(u.openid, '')), '') IS NOT NULL) AS has_wechat_openid_value,
    (NULLIF(TRIM(COALESCE(u.unionid, '')), '') IS NOT NULL) AS has_unionid_value,
    REGEXP_LIKE(COALESCE(u.openid, ''), '^[0-9a-f]{32}$', 'c') AS has_legacy_32hex_openid_shape,
    EXISTS (
      SELECT 1 FROM user_identities i
      WHERE i.user_id = u.id AND i.identity_type = 'phone'
        AND NULLIF(TRIM(i.identity_key), '') IS NOT NULL
        AND i.verified_at IS NOT NULL
    ) AS has_verified_phone_login,
    EXISTS (
      SELECT 1 FROM user_identities i
      WHERE i.user_id = u.id AND i.identity_type = 'phone'
        AND NULLIF(TRIM(i.identity_key), '') IS NOT NULL
        AND i.verified_at IS NULL
    ) AS has_unverified_phone_identity,
    (NULLIF(TRIM(COALESCE(u.password, '')), '') IS NOT NULL) AS has_password_credential,
    (NULLIF(TRIM(COALESCE(p.phone, '')), '') IS NOT NULL) AS has_profile_phone_value,
    (COALESCE(p.enterprise_wechat_verified, 0) = 1
      AND NULLIF(TRIM(COALESCE(p.enterprise_wechat_contact, '')), '') IS NOT NULL
    ) AS has_enterprise_verified_contact
  FROM users u
  LEFT JOIN user_profiles p ON p.user_id = u.id
  WHERE u.user_type = 'miniprogram'
)
SELECT account_created_date,
  COUNT(*) AS user_total,
  COALESCE(SUM(nickname_state = 'missing_profile'), 0) AS profile_missing,
  COALESCE(SUM(nickname_state = 'nickname_null'), 0) AS nickname_null,
  COALESCE(SUM(nickname_state = 'nickname_empty'), 0) AS nickname_empty,
  COALESCE(SUM(nickname_state = 'nickname_whitespace'), 0) AS nickname_whitespace,
  COALESCE(SUM(nickname_state = 'nickname_valid'), 0) AS nickname_valid,
  COALESCE(SUM(has_wechat_openid_value), 0) AS with_wechat_openid_value,
  COALESCE(SUM(has_unionid_value), 0) AS with_unionid_value,
  COALESCE(SUM(has_legacy_32hex_openid_shape), 0) AS with_legacy_32hex_openid_shape,
  COALESCE(SUM(has_verified_phone_login), 0) AS with_verified_phone_login,
  COALESCE(SUM(has_unverified_phone_identity), 0) AS with_unverified_phone_identity,
  COALESCE(SUM(has_password_credential), 0) AS with_password_credential,
  COALESCE(SUM(has_profile_phone_value AND NOT has_verified_phone_login), 0) AS profile_phone_without_verified_login,
  COALESCE(SUM(has_enterprise_verified_contact AND NOT has_verified_phone_login), 0) AS enterprise_contact_without_verified_login
FROM account_snapshot
GROUP BY account_created_date
ORDER BY account_created_date;

COMMIT;
