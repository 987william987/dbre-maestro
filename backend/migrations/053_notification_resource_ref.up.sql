ALTER TABLE notifications
    ADD COLUMN resource_ref VARCHAR(80) NULL AFTER resource_id;

UPDATE notifications n
INNER JOIN tickets t ON t.id = n.resource_id
SET n.resource_ref = t.ticket_no
WHERE n.resource_type = 'ticket'
  AND n.resource_ref IS NULL;
