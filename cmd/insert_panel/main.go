package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	html, err := os.ReadFile("web/dist/index.html")
	if err != nil {
		fmt.Println("ERROR read:", err)
		return
	}
	s := string(html)

	panel, err := os.ReadFile("web/dist/lc_panel.html")
	if err != nil {
		fmt.Println("ERROR read panel:", err)
		return
	}
	panelHTML := string(panel)

	// Insert lifecycle panel before "    </div>\n  `"
	// The template ends with: \n    </div>\n  `,
	// Find the LAST occurrence
	marker := "\r\n    </div>\r\n  `"
	idx := strings.LastIndex(s, marker)
	if idx < 0 {
		fmt.Println("ERROR: marker not found")
		// Try to find just </div>
		idx = strings.LastIndex(s, "</div>")
		fmt.Println("Last </div> at:", idx)
		return
	}
	s = s[:idx] + "\n      " + panelHTML + "\n    " + s[idx:]

	// Insert custom parameter form fields before "生成数量"
	formFields := "\n            <el-form-item label=\"申领人\"><el-input v-model=\"form.applicantName\" style=\"width:160px\" /></el-form-item>\n            <el-form-item label=\"工号\"><el-input v-model=\"form.applicantCode\" style=\"width:160px\" /></el-form-item>\n            <el-form-item label=\"手机号\"><el-input v-model=\"form.phone\" style=\"width:200px\" /></el-form-item>\n            <el-form-item label=\"使用区域\"><el-select v-model=\"form.factoryIds\" style=\"width:200px\"><el-option label=\"区域1\" value=\"3\" /><el-option label=\"区域2\" value=\"4\" /><el-option label=\"区域3\" value=\"5\" /><el-option label=\"区域4\" value=\"6\" /><el-option label=\"全部\" value=\"3,4,5,6\" /></el-select></el-form-item>\n            <el-form-item label=\"容量\"><el-select v-model=\"form.capacity\" style=\"width:200px\"><el-option label=\"16G\" value=\"16G\" /><el-option label=\"32G\" value=\"32G\" /><el-option label=\"64G\" value=\"64G\" /><el-option label=\"128G\" value=\"128G\" /></el-select></el-form-item>\n            <el-form-item label=\"格式\"><el-select v-model=\"form.format\" style=\"width:200px\"><el-option label=\"FAT32\" value=\"FAT32\" /><el-option label=\"exFAT\" value=\"exFAT\" /><el-option label=\"NTFS\" value=\"NTFS\" /></el-select></el-form-item>\n            <el-form-item label=\"使用时长(h)\"><el-input-number v-model=\"form.durationHours\" :min=\"1\" :max=\"720\" style=\"width:200px\" /></el-form-item>\n          "
	formMarker := "label=\"生成数量\">"
	idx = strings.Index(s, formMarker)
	if idx < 0 {
		fmt.Println("ERROR: form marker not found")
	} else {
		s = s[:idx] + formFields + s[idx:]
	}

	os.WriteFile("web/dist/index.html", []byte(s), 0644)
	fmt.Println("OK: HTML updated")
}