package main

// 临时排查工具：解密对比两份账号库里的 key 列表（不修改任何文件）。

import (
	"encoding/json"
	"fmt"
	"os"

	"skylogin/internal/secretfile"
)

type row map[string]any

func dump(p string) {
	b, err := secretfile.ReadFile(p)
	if err != nil {
		fmt.Printf("== %s 读取失败: %v\n", p, err)
		return
	}
	var list []row
	if err := json.Unmarshal(b, &list); err != nil {
		fmt.Printf("== %s 解析失败: %v\n", p, err)
		return
	}
	fmt.Printf("== %s（%d 条，加密=%v）\n", p, len(list), secretfile.IsEncrypted(p))
	for _, a := range list {
		tok, _ := a["token"].(string)
		if tok == "" {
			tok, _ = a["mpayToken"].(string)
		}
		fmt.Printf("   name=%-28q user=%-22v player=%-22v key=%-22v ch=%-10v source=%-12v region=%-8v token=%d\n",
			a["name"], a["userId"], a["playerId"], a["rowKey"], a["channel"], a["source"], a["region"], len(tok))
	}
}

func main() {
	for _, p := range os.Args[1:] {
		dump(p)
	}
}
