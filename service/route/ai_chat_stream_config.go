package route

import (
	"errors"
	"net/url"
	"strings"

	"service/model"
)

type chatStreamProvider struct {
	provider    string
	model       string
	endpoint    string
	apiKey      string
	completions bool
}

func resolveChatStreamProvider(config *model.AIAPIConfig, laozhangKey, arkKey string) (chatStreamProvider, error) {
	selected := chatStreamProvider{provider: "ark"}
	if laozhangKey != "" {
		selected.provider = "laozhang"
	}
	explicit := ""
	if config != nil {
		selected.endpoint = strings.TrimSpace(config.APIEndpoint)
		switch strings.ToLower(strings.TrimSpace(config.ProviderCode)) {
		case "laozhang":
			explicit = "laozhang"
		case "ark":
			explicit = "ark"
		}
	}
	if explicit != "" {
		selected.provider = explicit
	}
	parsed, err := url.Parse(selected.endpoint)
	if err != nil {
		return chatStreamProvider{}, errors.New("AI接口地址无效")
	}
	hostProvider := ""
	switch strings.ToLower(parsed.Hostname()) {
	case "api.laozhang.ai", "api2.laozhang.ai":
		hostProvider = "laozhang"
	case "ark.cn-beijing.volces.com":
		hostProvider = "ark"
	}
	if explicit != "" && hostProvider != "" && explicit != hostProvider {
		return chatStreamProvider{}, errors.New("AI供应商与接口主机不一致")
	}
	if hostProvider != "" {
		selected.provider = hostProvider
	}
	// 自定义主机无法推断供应商，保留显式供应商或原环境默认；路径仅决定协议。
	selected.completions = selected.provider == "laozhang"
	if selected.provider == "laozhang" {
		selected.apiKey, selected.model = laozhangKey, laozhangChatModel
	} else {
		selected.apiKey, selected.model = arkKey, arkModel
	}
	if selected.endpoint == "" {
		if selected.completions {
			selected.endpoint = laozhangChatBaseURL + "/chat/completions"
		} else {
			selected.endpoint = arkBaseURL + "/responses"
		}
	} else {
		switch path := strings.TrimRight(parsed.Path, "/"); {
		case strings.HasSuffix(path, "/chat/completions"):
			selected.completions = true
		case strings.HasSuffix(path, "/responses"):
			selected.completions = false
		}
	}
	if config != nil && strings.TrimSpace(config.APIKey) != "" {
		selected.apiKey = strings.TrimSpace(config.APIKey)
	}
	return selected, nil
}
