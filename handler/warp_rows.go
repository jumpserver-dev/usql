package handler

import (
	"database/sql"
	"github.com/jumpserver-dev/usql/feature"
	"strings"
)

// WarpRows 是对 sql.Rows 的包装，支持按列索引脱敏
type WarpRows struct {
	rows          *sql.Rows
	maskIndexes   []int
	temp          []interface{}
	dataMaskRules map[int]feature.DataMaskingRule
}

// NewWarpRows 构造函数
func NewWarpRows(rows *sql.Rows, maskIndexes []int) *WarpRows {
	return &WarpRows{
		rows:        rows,
		maskIndexes: maskIndexes,
	}
}

// Next 代理
func (w *WarpRows) Next() bool {
	return w.rows.Next()
}

// Scan 对行进行读取并按规则脱敏
func (w *WarpRows) Scan(dest ...interface{}) error {
	// 初始化 temp 缓存
	if w.temp == nil {
		w.temp = make([]interface{}, len(dest))
		for i := range w.temp {
			w.temp[i] = new(interface{})
		}
	}

	// 先 scan 到 temp
	if err := w.rows.Scan(w.temp...); err != nil {
		return err
	}

	// 遍历每一列，赋值到用户传入的 dest
	for i := range dest {
		src := *w.temp[i].(*interface{})
		if contains(w.maskIndexes, i) {
			rule, ok := w.dataMaskRules[i]
			val := rule.MaskPattern
			if ok {
				switch src.(type) {
				case []byte:
					val = replaceColumnVal(rule, string(src.([]byte)))
				case *string:
					val = replaceColumnVal(rule, *(src.(*string)))
				case *sql.NullString:
					if src.(*sql.NullString).Valid {
						val = replaceColumnVal(rule, src.(*sql.NullString).String)
					}
				}
				dest[i] = val
			}
		} else {
			dest[i] = src
		}
	}
	return nil

}

func replaceColumnVal(rule feature.DataMaskingRule, val string) string {
	switch rule.MaskingMethod {
	case feature.MaskingMethodFixedChar:
		// 固定字符替换
		if rule.MaskPattern == "" {
			return rule.MaskPattern
		}
		return rule.MaskPattern

	case feature.MaskingMethodHideMiddle:
		// 隐藏中间
		if len(val) < 3 {
			return rule.MaskPattern
		}
		return val[:1] + strings.Repeat("*", len(val)-2) + val[len(val)-1:]

	case feature.MaskingMethodKeepPrefix:
		// 保留前缀
		n := 2
		if n <= 0 || n >= len(val) {
			return "####"
		}
		return val[:n] + strings.Repeat("*", len(val)-n)

	case feature.MaskingMethodKeepSuffix:
		// 保留后缀
		n := 2
		if n <= 0 || n >= len(val) {
			return "####"
		}
		return strings.Repeat("*", len(val)-n) + val[len(val)-n:]
	default:
		// 未知策略
		return rule.MaskPattern
	}
}

// Columns 代理
func (w *WarpRows) Columns() ([]string, error) {
	return w.rows.Columns()
}

// ColumnTypes 代理
func (w *WarpRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return w.rows.ColumnTypes()
}

// Close 代理
func (w *WarpRows) Close() error {
	return w.rows.Close()
}

// Err 代理
func (w *WarpRows) Err() error {
	return w.rows.Err()
}

// NextResultSet 代理
func (w *WarpRows) NextResultSet() bool {
	return w.rows.NextResultSet()
}

// ---------------- 工具函数 ----------------

// contains 判断 index 是否在 maskIndexes 中
func contains(indexes []int, target int) bool {
	for _, i := range indexes {
		if i == target {
			return true
		}
	}
	return false
}
