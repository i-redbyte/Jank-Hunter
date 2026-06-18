package analyze

import "strings"

func ResolveOwnerAlias(ownerMap map[string]string, owner string) string {
	if len(ownerMap) == 0 || owner == "" {
		return owner
	}
	if mapped, ok := ownerMap[owner]; ok {
		return mapped
	}
	if hash, ok := ownerHash(owner); ok {
		if mapped, ok := ownerMap[hash]; ok {
			return mapped
		}
		if mapped, ok := ownerMap["jh:"+hash]; ok {
			return mapped
		}
	}
	return owner
}

func ownerHash(owner string) (string, bool) {
	if owner == "" {
		return "", false
	}
	if strings.HasPrefix(owner, "jh:") {
		return strings.TrimPrefix(owner, "jh:"), true
	}
	hashIndex := strings.LastIndex(owner, "#")
	if hashIndex < 0 || hashIndex == len(owner)-1 {
		return "", false
	}
	return owner[hashIndex+1:], true
}
