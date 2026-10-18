-- The public subscription page designs were replaced. Map each retired layout
-- to its closest successor so an installation keeps a comparable look:
-- pass/split -> ticket, compact -> minimal; portal keeps its name.
UPDATE settings SET value = CASE value
    WHEN 'pass' THEN 'ticket'
    WHEN 'split' THEN 'ticket'
    WHEN 'compact' THEN 'minimal'
    ELSE value
END
WHERE key = 'subscription.layout';
