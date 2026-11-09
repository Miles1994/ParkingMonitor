package main

type Region struct {
	Key         string
	Name        string
	MiniName    string
	Host        string
	Hostname    string
	AppID       string
	Version     string
	Mchid       string
	MiniAppID   string
	MiniVersion string
}

func (r Region) Referer() string {
	return "https://servicewechat.com/" + r.MiniAppID + "/" + r.MiniVersion + "/page-frame.html"
}

var regionList = []Region{
	{
		Key:         "gaoxin",
		Name:        "高新区",
		MiniName:    "西高新智慧停车",
		Host:        "https://gaoxin.zimoiot.com/",
		Hostname:    "gaoxin.zimoiot.com",
		AppID:       "wx686363a14b08832e",
		Version:     "1.4.6",
		Mchid:       "",
		MiniAppID:   "wx686363a14b08832e",
		MiniVersion: "60",
	},
	{
		Key:         "lianhu",
		Name:        "莲湖区",
		MiniName:    "莲湖智慧停车",
		Host:        "https://lianhu.zimoiot.com/",
		Hostname:    "lianhu.zimoiot.com",
		AppID:       "wx9aeee98af3ad4ba3",
		Version:     "1.5.10",
		Mchid:       "167",
		MiniAppID:   "wx486ce667a7d7b907",
		MiniVersion: "67",
	},
	{
		Key:         "bilin",
		Name:        "碑林区",
		MiniName:    "碑林智慧停车",
		Host:        "https://www.xablzhtc.com/",
		Hostname:    "www.xablzhtc.com",
		AppID:       "wx9aeee98af3ad4ba3",
		Version:     "1.5.10",
		Mchid:       "167",
		MiniAppID:   "wx9aeee98af3ad4ba3",
		MiniVersion: "102",
	},
}

func validRegion(key string) bool {
	for _, r := range regionList {
		if r.Key == key {
			return true
		}
	}
	return false
}

func regionByKey(key string) Region {
	for _, r := range regionList {
		if r.Key == key {
			return r
		}
	}
	return regionList[0]
}

func regionIndex(key string) int {
	for i, r := range regionList {
		if r.Key == key {
			return i
		}
	}
	return 0
}

var regionNames = func() []string {
	names := make([]string, len(regionList))
	for i, r := range regionList {
		names[i] = r.Name
	}
	return names
}()
