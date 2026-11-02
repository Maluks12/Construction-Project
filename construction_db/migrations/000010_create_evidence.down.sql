BEGIN;

DROP TABLE evidence_chain;
DROP TABLE site_chain_heads;
DROP TRIGGER evidence_freeze ON evidence;
DROP FUNCTION evidence_freeze();
DROP TABLE evidence;

COMMIT;
