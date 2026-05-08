package models

type APIResponse struct {
	IPAddress                string `json:"ipAddress"`
	Port                     int    `json:"port"`
	Remote                   string `json:"remote"`
	UnderMaintenance         int    `json:"underMaintenance"`
	MaintenanceRemainTimeSec int    `json:"maintenanceRemainTimeSec"`
	MaintenanceNewsURL       string `json:"maintenanceNewsURL"`
}
