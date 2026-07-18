CREATE TABLE jobs (
    stablekey   TEXT PRIMARY KEY,
    source      TEXT NOT NULL,
    posted_at   TIMESTAMPTZ,
    title       TEXT,
    company     TEXT,
    url         TEXT,
    payload     JSONB,                           -- marshaled Job struct
    has_applied BOOLEAN NOT NULL DEFAULT false,   -- required by Store iface, absent from CLAUDE.md's original sketch
    first_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE scores (
    id         BIGSERIAL PRIMARY KEY,
    stablekey  TEXT NOT NULL REFERENCES jobs(stablekey),
    posted_at  TIMESTAMPTZ,
    model      TEXT NOT NULL,
    score      DOUBLE PRECISION,
    sub_scores JSONB,
    raw        JSONB NOT NULL,
    logprobs   JSONB,
    reasoning  TEXT,                              -- derived scalar alongside raw
    run_kind   TEXT NOT NULL,                      -- 'main' | 'floor'
    scored_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON scores (stablekey, model, scored_at);
