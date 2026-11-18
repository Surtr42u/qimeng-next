package service

// Display membership and counters use the same authoritative relationships as
// facets. Clients do not reconstruct opaque keys, split names, or count a loaded
// history page. These projections are used by every asset list/detail read.
const assetWorkKeys = `CASE WHEN ` + assetIsCOS + ` THEN
 (SELECT json_group_array('author:'||author_id) FROM (SELECT x.author_id FROM asset_authors x JOIN authors facet_author ON facet_author.id=x.author_id WHERE x.asset_id=a.id AND json_extract(facet_author.data,'$.kind')='cos' ORDER BY x.author_id))
 WHEN EXISTS(SELECT 1 FROM asset_source_memberships WHERE asset_id=a.id) THEN
 (SELECT json_group_array('source:'||source_id) FROM (SELECT source_id FROM asset_source_memberships WHERE asset_id=a.id ORDER BY inferred,ordinal,source_id))
 ELSE '["source:__other__"]' END`
const assetRoleKeys = `CASE WHEN ` + assetIsCOS + ` THEN
 CASE WHEN a.cos_work_id!='' THEN json_array('work:'||a.cos_work_id) ELSE '[]' END
 ELSE (SELECT json_group_array('character:'||character_id) FROM (SELECT character_id FROM asset_characters WHERE asset_id=a.id ORDER BY ordinal,character_id)) END`
const assetWorkFacets = `CASE WHEN ` + assetIsCOS + ` THEN
 (SELECT json_group_array(json_object('key','author:'||id,'kind','author','id',id,'name',name)) FROM
 (SELECT au2.id,au2.name FROM asset_authors x JOIN authors au2 ON au2.id=x.author_id WHERE x.asset_id=a.id AND json_extract(au2.data,'$.kind')='cos' ORDER BY au2.id))
 WHEN EXISTS(SELECT 1 FROM asset_source_memberships WHERE asset_id=a.id) THEN
 (SELECT json_group_array(json_object('key','source:'||id,'kind','source','id',id,'name',name)) FROM
 (SELECT sr.id,sr.name FROM asset_source_memberships x JOIN sources sr ON sr.id=x.source_id WHERE x.asset_id=a.id ORDER BY x.inferred,x.ordinal,sr.id))
 ELSE '[{"key":"source:__other__","kind":"source","id":"__other__","name":"其他"}]' END`
const assetRoleFacets = `CASE WHEN ` + assetIsCOS + ` THEN
 (SELECT json_group_array(json_object('key','work:'||id,'kind','work','id',id,'name',name)) FROM cos_works WHERE id=a.cos_work_id)
 ELSE (SELECT json_group_array(json_object('key','character:'||id,'kind','character','id',id,'name',name)) FROM
 (SELECT ch.id,ch.name FROM asset_characters x JOIN characters ch ON ch.id=x.character_id WHERE x.asset_id=a.id ORDER BY x.ordinal,ch.id)) END`
const assetPlayback = `coalesce((SELECT json_object('positionMs',position_ms,'positionContentVersion',content_version)
 FROM asset_playback_positions WHERE asset_id=a.id ORDER BY content_version DESC LIMIT 1),
 '{"positionMs":0,"positionContentVersion":0}')`

const assetJSON = `json_set(json_patch(` + assetCoreJSON + `,` + assetPlayback + `),
 '$.viewCount',coalesce((SELECT sum(views) FROM asset_daily_stats WHERE asset_id=a.id),0),
 '$.playCount',coalesce((SELECT sum(plays) FROM asset_daily_stats WHERE asset_id=a.id),0),
 '$.cosWorkName',coalesce((SELECT name FROM cos_works WHERE id=a.cos_work_id),''),
 '$.workKeys',json(` + assetWorkKeys + `),'$.roleKeys',json(` + assetRoleKeys + `),
 '$.workFacets',json(` + assetWorkFacets + `),'$.roleFacets',json(` + assetRoleFacets + `))`
