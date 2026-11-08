// 高层操作：list / put 账号记录，对应参考实现 mpay_db_sync 的关键路径。
//
// 账号记录字段表来自参考实现 _create / packet_from_credentials 的反编译，
// 缺字段游戏侧会当成记录损坏跳过，所以一整套都要给。
package mpaydb

import (
	"encoding/json"
	"fmt"
	"os"
)

// AccountItem list_accounts 返回的单条记录（键为 game_id/login_channel/sdkuid）。
type AccountItem struct {
	GameID       string `json:"game_id"`
	LoginChannel string `json:"login_channel"`
	SDKUID       string `json:"sdkuid"`
}

// listResponse list_accounts 的 JSON 结果。
type listResponse struct {
	Accounts []AccountItem `json:"accounts"`
	Errors   []struct {
		RecordIndex int    `json:"record_index"`
		Reason      string `json:"reason"`
	} `json:"errors"`
}

// Credentials put 的凭证部分：token 是 MPay 登录凭证（1-eyJ…），
// pc_ext_info 是登录回调 slot8 里那段 extra_unisdk_data 的 JSON（可为空对象）。
type Credentials struct {
	Token     string         `json:"token"`
	PcExtInfo map[string]any `json:"pc_ext_info"`
}

// CreateRecord 新建记录的字段表（_create 反编译结果，一字不差）。
// __user_platform_selected 就是游戏"iOS/安卓选择框"的开关：空 = 要问，
// 填 ad / ios = 不问。
type CreateRecord struct {
	LoginChannel               string         `json:"login_channel"`
	LoginType                  int            `json:"login_type"`
	UserLoginMethod            int            `json:"__user_login_method"`
	UserPlatformSelected       string         `json:"__user_platform_selected"`
	UserPlatformSelectedSecond string         `json:"__user_platform_selected_second"`
	UserPlatformSelectedThird  string         `json:"__user_platform_selected_third"`
	UserTypedUsername          string         `json:"__user_typed_username"`
	ClientUsername             string         `json:"client_username"`
	DisplayUsername            string         `json:"display_username"`
	Nickname                   string         `json:"nickname"`
	Avatar                     string         `json:"avatar"`
	ExtAccessToken             string         `json:"ext_access_token"`
	NeedMask                   bool           `json:"need_mask"`
	PcExtInfo                  map[string]any `json:"pc_ext_info"`
	RelatedLoginStatus         int            `json:"related_login_status"`
	MaskRelatedMobile          string         `json:"mask_related_mobile"`
}

// PutArgs put_account 的完整参数（参考实现 promote_account / _write_record）。
// Position 0 = 列表头部（游戏登录窗里排最前）。
type PutArgs struct {
	GameID            string       `json:"game_id"`
	LoginChannel      string       `json:"login_channel"`
	SDKUID            string       `json:"sdkuid"`
	Name              string       `json:"name"`
	Position          int          `json:"position"`
	MachineIdentifier string       `json:"machine_identifier"`
	Credentials       Credentials  `json:"credentials"`
	Create            CreateRecord `json:"create"`
}

// ListAccounts 读出库里现有的账号记录。
func ListAccounts(db *DB, dbPath string) ([]AccountItem, error) {
	original, err := os.ReadFile(dbPath)
	if err != nil {
		return nil, err
	}
	_, _, result, err := db.Invoke(OpList, original, map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp listResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("解析 list_accounts 结果失败: %w", err)
	}
	return resp.Accounts, nil
}

// PutAccount 把一条账号记录写进库（存在则覆盖），返回库文件是否有变化。
// 写成功时把新库原子落盘（临时文件 + rename，和官方写法等价）。
func PutAccount(db *DB, dbPath string, args PutArgs) (changed bool, err error) {
	original, err := os.ReadFile(dbPath)
	if err != nil {
		return false, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return false, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false, err
	}
	changed, newDB, _, err := db.Invoke(OpPut, original, m)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	tmp := dbPath + ".tmp"
	if err := os.WriteFile(tmp, newDB, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		return false, err
	}
	return true, nil
}
