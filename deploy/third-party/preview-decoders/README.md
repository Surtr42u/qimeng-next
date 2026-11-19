# 展示缩略图解码器的许可与署名

JPEG 解码器使用 go-scaled-jpeg v1.2.0，提交 `0725b8baf8893dcf98ec85167077a0a16a9c3aae`。PNG 解码器使用 go-scaled-png v1.0.0，提交 `3888559f36b39ff162168624a4dd9688849cfb7a`。[JPEG 上游](https://github.com/m8rge/go-scaled-jpeg/tree/0725b8baf8893dcf98ec85167077a0a16a9c3aae) · [PNG 上游](https://github.com/m8rge/go-scaled-png/tree/3888559f36b39ff162168624a4dd9688849cfb7a)

| 随附文件 | 来源与适用部分 |
|---|---|
| [LICENSE](LICENSE) · [LICENSE.GOLANG](LICENSE.GOLANG) | 两个模块各自的同名原件逐字相同。MIT 条款覆盖 Andrew Putilov 的代码，Go Authors 的 BSD 三条款覆盖出自 Go 标准库的部分。 |
| [COPYING.IJG](COPYING.IJG) | JPEG 模块对 Independent JPEG Group 反变换代码的许可和致谢。[上游原件](https://github.com/m8rge/go-scaled-jpeg/blob/0725b8baf8893dcf98ec85167077a0a16a9c3aae/COPYING.IJG) |
| [IJG-README.txt](IJG-README.txt) | JPEG 模块的 `idct.go` 指向 IJG v9f。本文件是官方源码包中的完整 `jpeg-9f/README`，分发条件见其中的 LEGAL ISSUES。[上游引用](https://github.com/m8rge/go-scaled-jpeg/blob/0725b8baf8893dcf98ec85167077a0a16a9c3aae/idct.go#L3-L15) · [IJG v9f 源码包](https://www.ijg.org/files/jpegsrc.v9f.tar.gz) |
| [LICENSE.imaging](LICENSE.imaging) | PNG 滤镜保留了 Grigory Dryapak 的代码。本文件是该模块所引 imaging v1.6.4 的 MIT 许可原件。[PNG 滤镜](https://github.com/m8rge/go-scaled-png/blob/3888559f36b39ff162168624a4dd9688849cfb7a/filters.go) · [滤镜实现](https://github.com/kovidgoyal/imaging/blob/9f2dbdd75748296123dd85ec7ab2509b0037b6a0/resize.go#L347) · [许可原件](https://github.com/kovidgoyal/imaging/blob/9f2dbdd75748296123dd85ec7ab2509b0037b6a0/LICENSE) |
| [SOURCE-NOTICES.txt](SOURCE-NOTICES.txt) | 按原路径摘录 Go Authors、Thomas G. Lane、Guido Vollbeding 和 Grigory Dryapak 的源码署名，以及 JPEG 保留的 MPEG Software Simulation Group 完整声明。[MPEG 声明](https://github.com/m8rge/go-scaled-jpeg/blob/0725b8baf8893dcf98ec85167077a0a16a9c3aae/idct_orig.go#L13-L38) |

This software is based in part on the work of the Independent JPEG Group.
