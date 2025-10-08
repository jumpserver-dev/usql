package feature

type DataMaskingRule struct {
	Name          string `json:"name"`
	FieldsPattern string `json:"fields_pattern"`
	MaskPattern   string `json:"mask_pattern"`
	MaskingMethod string `json:"masking_method"`
}

const DataMaskingKey = "data-masking-rules"

//fixed_char = "fixed_char", _("Fixed Character Replacement")  # 固定字符替换
//hide_middle = "hide_middle", _("Hide Middle Characters")  # 隐藏中间几位
//keep_prefix = "keep_prefix", _("Keep Prefix Only")  # 只保留前缀
//keep_suffix = "keep_suffix", _("Keep Suffix Only")  # 只保留后缀

const (
	MaskingMethodFixedChar  = "fixed_char"
	MaskingMethodHideMiddle = "hide_middle"
	MaskingMethodKeepPrefix = "keep_prefix"
	MaskingMethodKeepSuffix = "keep_suffix"
)
