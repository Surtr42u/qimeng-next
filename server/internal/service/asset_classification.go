package service

// The library selects directory interpretation, not the browse partition. A
// media item is COS exactly when an associated author is COS. This also covers
// root-level unclassified files and COS items with additional regular authors.
// All projections below use the canonical asset alias a.
const assetIsCOS = `EXISTS(SELECT 1 FROM asset_authors partition_link JOIN authors partition_author ON partition_author.id=partition_link.author_id WHERE partition_link.asset_id=a.id AND json_extract(partition_author.data,'$.kind')='cos')`
const assetPartition = `(CASE WHEN ` + assetIsCOS + ` THEN 'cos' ELSE 'regular' END)`
