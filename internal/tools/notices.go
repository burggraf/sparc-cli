package tools

import _ "embed"

//go:embed THIRD_PARTY_NOTICES.txt
var thirdPartyNotices string

// ThirdPartyNotices returns licenses shipped with the bundled PostgreSQL client.
func ThirdPartyNotices() string { return thirdPartyNotices }
