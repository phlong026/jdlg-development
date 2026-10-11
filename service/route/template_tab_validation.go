package route

import (
	"fmt"
	"strings"

	"service/model"
)

var sharedTemplateSquareConfigModel *model.TemplateSquareConfigModel

func setSharedTemplateSquareConfigModel(templateSquareConfigModel *model.TemplateSquareConfigModel) {
	sharedTemplateSquareConfigModel = templateSquareConfigModel
}

func validateTemplateSquareConfig(mainTabs []model.TabItem, subTabs []model.TabItem, thirdTabs []model.TabItem) error {
	if len(mainTabs) == 0 {
		return fmt.Errorf("至少保留一个一级Tab")
	}

	mainValues := make(map[string]struct{}, len(mainTabs))
	for index, item := range mainTabs {
		label := strings.TrimSpace(item.Label)
		value := strings.TrimSpace(item.Value)
		if label == "" {
			return fmt.Errorf("第 %d 个一级Tab缺少显示名", index+1)
		}
		if value == "" {
			return fmt.Errorf("第 %d 个一级Tab缺少 value", index+1)
		}
		if _, exists := mainValues[value]; exists {
			return fmt.Errorf("一级Tab的 value 不能重复: %s", value)
		}
		mainValues[value] = struct{}{}
	}

	subValues := make(map[string]string, len(subTabs))
	for index, item := range subTabs {
		label := strings.TrimSpace(item.Label)
		value := strings.TrimSpace(item.Value)
		parent := strings.TrimSpace(item.Parent)
		if label == "" {
			return fmt.Errorf("第 %d 个二级Tab缺少显示名", index+1)
		}
		if value == "" {
			return fmt.Errorf("第 %d 个二级Tab缺少 value", index+1)
		}
		if parent == "" {
			return fmt.Errorf("第 %d 个二级Tab必须设置所属的一级Tab", index+1)
		}
		if _, exists := mainValues[parent]; !exists {
			return fmt.Errorf("第 %d 个二级Tab的父Tab（%s）不存在于一级Tab列表中", index+1, parent)
		}
		if _, exists := subValues[value]; exists {
			return fmt.Errorf("二级Tab的 value 不能重复: %s", value)
		}
		subValues[value] = parent
	}

	thirdValues := make(map[string]string, len(thirdTabs))
	for index, item := range thirdTabs {
		label := strings.TrimSpace(item.Label)
		value := strings.TrimSpace(item.Value)
		parent := strings.TrimSpace(item.Parent)
		if label == "" {
			return fmt.Errorf("第 %d 个三级Tab缺少显示名", index+1)
		}
		if value == "" {
			return fmt.Errorf("第 %d 个三级Tab缺少 value", index+1)
		}
		if parent == "" {
			return fmt.Errorf("第 %d 个三级Tab必须设置所属的二级Tab", index+1)
		}
		if _, exists := subValues[parent]; !exists {
			return fmt.Errorf("第 %d 个三级Tab的父Tab（%s）不存在于二级Tab列表中", index+1, parent)
		}
		if _, exists := thirdValues[value]; exists {
			return fmt.Errorf("三级Tab的 value 不能重复: %s", value)
		}
		thirdValues[value] = parent
	}

	return nil
}

func validateTemplateTabAssignment(templateSquareConfigModel *model.TemplateSquareConfigModel, mainTab string, subTab string, thirdTab string) error {
	trimmedMain := strings.TrimSpace(mainTab)
	trimmedSub := strings.TrimSpace(subTab)
	trimmedThird := strings.TrimSpace(thirdTab)

	if trimmedMain == "" && trimmedSub == "" && trimmedThird == "" {
		return nil
	}
	if trimmedMain == "" {
		return fmt.Errorf("未选择一级Tab时不能设置二级或三级Tab")
	}
	if templateSquareConfigModel == nil {
		return fmt.Errorf("Tab 配置未就绪")
	}

	cfg, err := templateSquareConfigModel.Get()
	if err != nil {
		return fmt.Errorf("读取 Tab 配置失败: %w", err)
	}
	if cfg == nil {
		return fmt.Errorf("Tab 配置不存在")
	}

	mainTabs, err := templateSquareConfigModel.ParseMainTabs(cfg.MainTabs)
	if err != nil {
		return fmt.Errorf("解析一级Tab配置失败: %w", err)
	}
	subTabs, err := templateSquareConfigModel.ParseSubTabs(cfg.SubTabs)
	if err != nil {
		return fmt.Errorf("解析二级Tab配置失败: %w", err)
	}
	thirdTabs, err := templateSquareConfigModel.ParseThirdTabs(cfg.ThirdTabs)
	if err != nil {
		return fmt.Errorf("解析三级Tab配置失败: %w", err)
	}

	mainValues := make(map[string]struct{}, len(mainTabs))
	for _, item := range mainTabs {
		value := strings.TrimSpace(item.Value)
		if value != "" {
			mainValues[value] = struct{}{}
		}
	}
	if _, exists := mainValues[trimmedMain]; !exists {
		return fmt.Errorf("一级Tab不存在: %s", trimmedMain)
	}

	if trimmedSub == "" {
		if trimmedThird != "" {
			return fmt.Errorf("未选择二级Tab时不能设置三级Tab")
		}
		return nil
	}

	subParents := make(map[string]string, len(subTabs))
	for _, item := range subTabs {
		value := strings.TrimSpace(item.Value)
		parent := strings.TrimSpace(item.Parent)
		if value != "" {
			subParents[value] = parent
		}
	}
	if parent, exists := subParents[trimmedSub]; !exists {
		return fmt.Errorf("二级Tab不存在: %s", trimmedSub)
	} else if parent != trimmedMain {
		return fmt.Errorf("二级Tab %s 不属于一级Tab %s", trimmedSub, trimmedMain)
	}

	if trimmedThird == "" {
		return nil
	}

	thirdParents := make(map[string]string, len(thirdTabs))
	for _, item := range thirdTabs {
		value := strings.TrimSpace(item.Value)
		parent := strings.TrimSpace(item.Parent)
		if value != "" {
			thirdParents[value] = parent
		}
	}
	if parent, exists := thirdParents[trimmedThird]; !exists {
		return fmt.Errorf("三级Tab不存在: %s", trimmedThird)
	} else if parent != trimmedSub {
		return fmt.Errorf("三级Tab %s 不属于二级Tab %s", trimmedThird, trimmedSub)
	}

	return nil
}
