BEGIN;

-- A regulatory regime is the body of law a site operates under.
-- Everything regime-specific (checklists, deadlines, vocabulary, forms)
-- will hang off this table as data, so the core never needs
-- "if edition == ..." in Go code. See Rev 4, section 1.4.
CREATE TABLE regulatory_regimes (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code           text NOT NULL UNIQUE,
    name           text NOT NULL,
    edition        text NOT NULL CHECK (edition IN ('construction', 'mining')),
    jurisdiction   char(2) NOT NULL DEFAULT 'ZA',
    version        text NOT NULL,
    effective_from date NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- Seed data: regimes are part of the system, not created by users.
-- Construction Regulations, 2014 (GN R84, Government Gazette 37305).
-- Commenced 7 February 2014, except regulations 3 and 5(7)(b),
-- which commenced 8 August 2015.
INSERT INTO regulatory_regimes (code, name, edition, jurisdiction, version, effective_from)
VALUES ('za_construction_2014', 'Construction Regulations, 2014', 'construction', 'ZA', '2014', '2014-02-07');

COMMIT;