# R8 只对 release 生效（debug 保持不混淆）。这里只保留“按字段名/注解名反射”的契约；
# 其余优化（裁剪、内联、去分支、DEX 重排）全开，正是旧版 release 与 debug 差距的一部分。
#
# 1) Gson 反序列化的应用模型：这些类没有 @SerializedName，字段名本身就是落盘/线上的键
#    （连接配置、外观偏好、队列日志、维护/设备领取记录）。混淆字段名会让旧记录读不回来。
-keep class media.qimeng.next.model.** { *; }
-keepclassmembers class media.qimeng.next.data.** { <fields>; }

# 2) 生成 SDK 的 DTO 全部带 @SerializedName（Gson 自带 consumer 规则覆盖），这里再显式兜一层，
#    避免以后换 converter 时默认假设失效。
-keepclassmembers,allowobfuscation class * {
    @com.google.gson.annotations.SerializedName <fields>;
}

# 3) Retrofit 服务接口：@HTTP 注解 + 泛型签名决定请求；库自带规则之外显式写一遍便于审查。
-keepattributes Signature,InnerClasses,EnclosingMethod
-keepattributes RuntimeVisibleAnnotations,RuntimeVisibleParameterAnnotations,AnnotationDefault
-keep,allowobfuscation interface media.qimeng.next.sdk.api.**

# 4) 枚举按名字读写（外观材质、上传模式、传输状态等落盘值）。
-keepclassmembers enum * {
    public static **[] values();
    public static ** valueOf(java.lang.String);
}

# 5) coil-network-okhttp 3.1.0 的 android 产物里，OkHttpNetworkFetcherFactory 的默认参数
#    字面量仍带着 KMP 公共签名 coil3.PlatformContext；它在 Android 侧是 typealias（成品里
#    只有 PlatformContextKt，没有同名类），所以 R8 会报 missing class 并中止 release。
#    我们始终显式传入 connectivityChecker，那条默认参数不会被构造，放过这条悬空引用即可。
-dontwarn coil3.PlatformContext
