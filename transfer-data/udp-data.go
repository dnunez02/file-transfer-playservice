package transferdata

type UDP_Pack struct {
	Id        int64
	Num_Packs int64
	Data      TCP_Pack
}

func ReformatToUDPPacket(tcp_packs []TCP_Pack) []UDP_Pack {
	result := make([]UDP_Pack, len(tcp_packs))
	num_packs := int64(len(tcp_packs))
	for idx, tcp_pack := range tcp_packs {
		result[idx] = UDP_Pack{int64(idx) + 1, num_packs, tcp_pack}
	}
	return result
}
