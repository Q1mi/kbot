# 车险反欺诈规则集 auto-fraud-2026.08

## 高风险信号

- `EVENT_BEFORE_POLICY_EFFECTIVE`：事故时间早于保单生效时间。
- `DUPLICATE_DAMAGE_IMAGE`：事故图片摘要与历史案件重复。
- `RELATED_PAYMENT_ACCOUNT`：收款账户关联多个无关投保人。
- `HIGH_FREQUENCY_CLAIMS`：车辆或投保人在短周期内出现多次案件。

风险分达到 0.70 时进入人工调查路径。风险信号仅用于分流和调查，拒赔结论仍需责任规则、证据和人工审核共同支持。
