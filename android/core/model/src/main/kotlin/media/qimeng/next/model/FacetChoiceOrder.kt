package media.qimeng.next.model

/** 「其他」桶显示名（服务端兜底桶；与旧版 Android `FacetOption.OTHER_BUCKET_NAME` 同名同字）。 */
private const val OTHER_BUCKET_NAME = "其他"

/**
 * 候选行置底：「其他」桶恒排最末，其余项保持传入顺序（稳定排序，非计数重排）。
 *
 * 来源是旧版 Android `android/core/model/.../FacetOption.kt` 的
 * `List<FacetOption>.withOtherBucketLast()`：旧版在作品行与角色行对「其他」兜底桶
 * （无作品/角色归属）统一置底，筛选胶囊「全部」仍在最前。服务端只按计数降序返回候选
 * （`server/internal/service/asset_facets.go` 的 `facet_count` 排序），协议
 * （`api/openapi.yaml` 的 `AssetFacetPage`）没有承诺兜底桶的位置，因此客户端在显示层兜底。
 *
 * 纯函数，只改显示顺序、不增删项。调用方必须用同一个置底后的列表同时渲染胶囊和按下标
 * 回查点击项，否则选中态会和对错位。
 */
fun List<FacetChoice>.withOtherBucketLast(): List<FacetChoice> {
    val (others, rest) = partition { it.name == OTHER_BUCKET_NAME }
    return rest + others
}
