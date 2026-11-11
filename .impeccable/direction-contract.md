<!--
DIRECTION CONTRACT · taboo 控制台重构 · seed 9ee163f2 · mode operate
THESIS: 密钥是一枚被封印的东西，界面是那道禁制本身。拒绝「GitHub 深色 + 卡片堆」的类别默认：没有一套同尺寸卡片网格、没有渐变文字、没有霓虹发光；深度来自墨底的分层与结界线，强调只来自朱砂。
OWN-WORLD: 墨阶（#0a0907 → #353025）+ 白瓷前景 + 单一朱砂 #d4432e（印、危险、当前、计数）；状态只用玉 #6db98a 与琥珀 #d59a3a。排版：Inter 系 UI 字体 13/14px 基线与等宽字体承载一切标识符与密文；「禁」字印章是唯一的品牌标记；图标为自绘 1.5px SVG；1px 结界线取代阴影，圆角 3–8px，弹层与抽屉才允许抬起。
STORY: 操作者进入时先看到「哪道禁制、谁的印、现在是否被破」，再看到密文本身——密钥名与掩码长度先给形状，显示与复制是显式动作且 20 秒后自愈；审计是与控制台并行的第二条时间线，而不是设置里的一个子页。
FIRST VIEWPORT: 顶栏（印章 + taboo 禁制 + 组织/项目上下文切换 + 会话印章状态 + 头像菜单）；左侧印台侧栏按「密钥面 / 治理 / 印记」分组；主区一把密钥一张表：等宽 Key、●●●● 密文、悬停浮出的显示/复制/编辑/删除行内动作，右侧是详情抽屉（版本时间线）而非模态。小屏：侧栏收为底栏四格，表格变卡片行，动作常显。
FORM: 有序清单第 5 位（被抽取的可建方向）：「夜行 / 结界」——一个把危险、遮蔽与留痕变成空间秩序的运维界面。seed key 9ee163f2。
RAISES: Saville 目录（挑战者 6）→ 密文长度作为信息本身（●●●● 疏密），与极少的全彩只给当前项；暗房曝光记录（挑战者 1）→ 区域用不同密度的等宽符号承载状态，而不是用颜色堆积；jet-age 机票钱包（挑战者 2）→ 一次性秘密以「凭证」呈现并自我作废，而不是一条 toast。
FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
-->
