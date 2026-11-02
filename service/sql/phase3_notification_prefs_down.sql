-- Reverse of phase3_notification_prefs_up.sql. Dropping the table returns
-- every recipient to "all categories enabled" delivery behavior.
DROP TABLE IF EXISTS notification_preferences;
