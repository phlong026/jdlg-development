-- Disable new house-report generation and stop workers before rollback.
-- Archive the table and its private asset directory first, especially submitting
-- receipts: deleting them while generation remains enabled can repeat charges.
DROP TABLE IF EXISTS report_house_images;
DROP TABLE IF EXISTS report_house_image_storage;
