package model

import (
	"encoding/json"

	"gorm.io/datatypes"
)

// DecodeJSONStringList 把 JSON 字符串数组解出来。空值和 null 都当成没有配置。
func DecodeJSONStringList(raw datatypes.JSON) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}
