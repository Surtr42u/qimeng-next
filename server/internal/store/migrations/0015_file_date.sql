-- File-date is a projection of the currently verified original, not an input
-- clue left over from an earlier upload of the same asset identity.
UPDATE assets SET
 file_date=CAST(unixepoch(COALESCE(json_extract(data,'$.contentMtime'),json_extract(data,'$.sourceMtime')),'subsec')*1000 AS INTEGER),
 data=json_set(data,'$.fileDate',COALESCE(json_extract(data,'$.contentMtime'),json_extract(data,'$.sourceMtime')));
