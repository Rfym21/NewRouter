package model

import "sort"

// GetPriceSyncModelNames returns concrete models configured on this site. Price
// defaults and metadata prefix/contains/suffix rules are not model instances.
func GetPriceSyncModelNames() ([]string, error) {
	var routed, catalogued []string
	if err := DB.Table("abilities").
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Distinct("abilities.model").Pluck("abilities.model", &routed).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&Model{}).Where("name_rule = ?", NameRuleExact).
		Pluck("model_name", &catalogued).Error; err != nil {
		return nil, err
	}
	names := normalizeLookupValues(append(routed, catalogued...))
	sort.Strings(names)
	return names, nil
}
