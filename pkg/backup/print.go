package backup

import (
	"encoding/json"
	"fmt"
	"github.com/AlexAkulov/clickhouse-backup/pkg/config"
	apexLog "github.com/apex/log"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/AlexAkulov/clickhouse-backup/pkg/clickhouse"
	"github.com/AlexAkulov/clickhouse-backup/pkg/metadata"
	"github.com/AlexAkulov/clickhouse-backup/pkg/storage"
	"github.com/AlexAkulov/clickhouse-backup/pkg/utils"
)

func printBackupsRemote(w io.Writer, backupList []storage.Backup, format string) error {
	switch format {
	case "latest", "last", "l":
		if len(backupList) < 1 {
			return fmt.Errorf("no backups found")
		}
		fmt.Println(backupList[len(backupList)-1].BackupName)
	case "penult", "prev", "previous", "p":
		if len(backupList) < 2 {
			return fmt.Errorf("no penult backup is found")
		}
		fmt.Println(backupList[len(backupList)-2].BackupName)
	case "all", "":
		// if len(backupList) == 0 {
		// 	fmt.Println("no backups found")
		// }
		for _, backup := range backupList {
			size := utils.FormatBytes(backup.DataSize + backup.MetadataSize)
			if backup.CompressedSize > 0 {
				size = utils.FormatBytes(backup.CompressedSize + backup.MetadataSize)
			}
			description := backup.DataFormat
			uploadDate := backup.UploadDate.Format("02/01/2006 15:04:05")
			if backup.Legacy {
				description = "old-format"
			}
			required := ""
			if backup.RequiredBackup != "" {
				required = "+" + backup.RequiredBackup
			}
			if backup.Broken != "" {
				description = backup.Broken
				size = "???"
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", backup.BackupName, size, uploadDate, "remote", required, description); err != nil {
				apexLog.Warnf("can't write remote backup list to writer: %v", err)
			}
		}
	default:
		return fmt.Errorf("'%s' undefined", format)
	}
	return nil
}

func printBackupsLocal(w io.Writer, backupList []BackupLocal, format string) error {
	switch format {
	case "latest", "last", "l":
		if len(backupList) < 1 {
			return fmt.Errorf("no backups found")
		}
		fmt.Println(backupList[len(backupList)-1].BackupName)
	case "penult", "prev", "previous", "p":
		if len(backupList) < 2 {
			return fmt.Errorf("no penult backup is found")
		}
		fmt.Println(backupList[len(backupList)-2].BackupName)
	case "all", "":
		// if len(backupList) == 0 {
		// 	fmt.Println("no backups found")
		// }
		for _, backup := range backupList {
			size := utils.FormatBytes(backup.DataSize + backup.MetadataSize)
			if backup.CompressedSize > 0 {
				size = utils.FormatBytes(backup.CompressedSize + backup.MetadataSize)
			}
			description := backup.DataFormat
			creationDate := backup.CreationDate.Format("02/01/2006 15:04:05")
			if backup.Legacy {
				size = "???"
			}
			required := ""
			if backup.RequiredBackup != "" {
				required = "+" + backup.RequiredBackup
			}
			if backup.Broken != "" {
				description = backup.Broken
				size = "???"
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", backup.BackupName, size, creationDate, "local", required, description); err != nil {
				apexLog.Warnf("can't write local backup list to writer: %v", err)
			}
		}
	default:
		return fmt.Errorf("'%s' undefined", format)
	}
	return nil
}

// PrintLocalBackups - print all backups stored locally
func PrintLocalBackups(cfg *config.Config, format string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.DiscardEmptyColumns)
	defer func() {
		if err := w.Flush(); err != nil {
			apexLog.Warnf("can't flush tabwriter for local backup list: %v ", err)
		}
	}()
	backupList, _, err := GetLocalBackups(cfg, nil)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return printBackupsLocal(w, backupList, format)
}

// GetLocalBackups - return slice of all backups stored locally
func GetLocalBackups(cfg *config.Config, disks []clickhouse.Disk) ([]BackupLocal, []clickhouse.Disk, error) {
	var err error
	ch := &clickhouse.ClickHouse{
		Config: &cfg.ClickHouse,
	}
	if err = ch.Connect(); err != nil {
		apexLog.Warnf("can't connect to clickhouse: %v will try get backup directly from /var/lib/clickhouse/", err)
	} else {
		defer ch.Close()
	}
	if disks == nil && err == nil {
		disks, err = ch.GetDisks()
		if err != nil {
			return nil, nil, err
		}
	} else if disks == nil && err != nil {
		disks = []clickhouse.Disk{
			{Name: "default", Path: "/var/lib/clickhouse"},
		}
	}
	dataPath, err := ch.GetDefaultPath(disks)
	if err != nil {
		return nil, nil, err
	}
	result := make([]BackupLocal, 0)
	backupsPath := path.Join(dataPath, "backup")
	d, err := os.Open(backupsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, disks, nil
		}
		return nil, nil, err
	}
	defer func() {
		if err := d.Close(); err != nil {
			apexLog.Warnf("can't close backupPath for GetLocalBackups: %v ", err)
		}
	}()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return nil, nil, err
	}
	for _, name := range names {
		info, err := os.Stat(path.Join(backupsPath, name))
		if err != nil {
			continue
		}
		if !info.IsDir() {
			continue
		}
		backupMetafilePath := path.Join(backupsPath, name, "metadata.json")
		backupMetadataBody, err := os.ReadFile(backupMetafilePath)
		if os.IsNotExist(err) {
			// Legacy backup
			result = append(result, BackupLocal{
				BackupMetadata: metadata.BackupMetadata{
					BackupName:   name,
					CreationDate: info.ModTime(),
				},
				Legacy: true,
			})
			continue
		}
		var backupMetadata metadata.BackupMetadata
		if err := json.Unmarshal(backupMetadataBody, &backupMetadata); err != nil {
			return nil, disks, err
		}
		result = append(result, BackupLocal{
			BackupMetadata: backupMetadata,
			Legacy:         false,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CreationDate.Before(result[j].CreationDate)
	})
	return result, disks, nil
}

func PrintAllBackups(cfg *config.Config, format string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.DiscardEmptyColumns)
	defer func() {
		if err := w.Flush(); err != nil {
			apexLog.Warnf("can't flush tabwriter for PrintAllBackups: %v ", err)
		}
	}()
	localBackups, _, err := GetLocalBackups(cfg, nil)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = printBackupsLocal(w, localBackups, format); err != nil {
		apexLog.Warnf("printBackupsLocal return error: %v", err)
	}

	if cfg.General.RemoteStorage != "none" {
		remoteBackups, err := GetRemoteBackups(cfg, true)
		if err != nil {
			return err
		}
		if err = printBackupsRemote(w, remoteBackups, format); err != nil {
			apexLog.Warnf("printBackupsLocal return error: %v", err)
		}
	}
	return nil
}

// PrintRemoteBackups - print all backups stored on remote storage
func PrintRemoteBackups(cfg *config.Config, format string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.DiscardEmptyColumns)
	defer func() {
		if err := w.Flush(); err != nil {
			apexLog.Warnf("can't flush tabwriter for PrintRemoteBackups: %v ", err)
		}
	}()
	backupList, err := GetRemoteBackups(cfg, true)
	if err != nil {
		return err
	}
	return printBackupsRemote(w, backupList, format)
}

func getLocalBackup(cfg *config.Config, backupName string, disks []clickhouse.Disk) (*BackupLocal, []clickhouse.Disk, error) {
	if backupName == "" {
		return nil, disks, fmt.Errorf("backup name is required")
	}
	backupList, disks, err := GetLocalBackups(cfg, disks)
	if err != nil {
		return nil, disks, err
	}
	for _, backup := range backupList {
		if backup.BackupName == backupName {
			return &backup, disks, nil
		}
	}
	return nil, disks, fmt.Errorf("backup '%s' is not found", backupName)
}

// GetRemoteBackups - get all backups stored on remote storage
func GetRemoteBackups(cfg *config.Config, parseMetadata bool) ([]storage.Backup, error) {
	if cfg.General.RemoteStorage == "none" {
		return nil, fmt.Errorf("remote_storage is 'none'")
	}
	bd, err := storage.NewBackupDestination(cfg, false)
	if err != nil {
		return []storage.Backup{}, err
	}
	if err := bd.Connect(); err != nil {
		return []storage.Backup{}, err
	}
	backupList, err := bd.BackupList(parseMetadata, "")
	if err != nil {
		return []storage.Backup{}, err
	}
	// ugly hack to fix https://github.com/AlexAkulov/clickhouse-backup/issues/309
	if parseMetadata == false && len(backupList) > 0 {
		lastBackup := backupList[len(backupList)-1]
		backupList, err = bd.BackupList(true, lastBackup.BackupName)
		if err != nil {
			return []storage.Backup{}, err
		}
	}
	return backupList, err
}

// GetTables - get all tables for use by PrintTables and API
func GetTables(cfg *config.Config) ([]clickhouse.Table, error) {
	ch := &clickhouse.ClickHouse{
		Config: &cfg.ClickHouse,
	}

	if err := ch.Connect(); err != nil {
		return []clickhouse.Table{}, fmt.Errorf("can't connect to clickhouse: %v", err)
	}
	defer ch.Close()

	allTables, err := ch.GetTables("")
	if err != nil {
		return []clickhouse.Table{}, fmt.Errorf("can't get tables: %v", err)
	}
	return allTables, nil
}

// PrintTables - print all tables suitable for backup
func PrintTables(cfg *config.Config, printAll bool) error {
	ch := &clickhouse.ClickHouse{
		Config: &cfg.ClickHouse,
	}
	if err := ch.Connect(); err != nil {
		return err
	}
	defer ch.Close()

	allTables, err := GetTables(cfg)
	if err != nil {
		return err
	}
	disks, err := ch.GetDisks()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.DiscardEmptyColumns)
	for _, table := range allTables {
		if table.Skip && !printAll {
			continue
		}
		tableDisks := []string{}
		for disk := range clickhouse.GetDisksByPaths(disks, table.DataPaths) {
			tableDisks = append(tableDisks, disk)
		}
		if table.Skip {
			if _, err := fmt.Fprintf(w, "%s.%s\t%s\t%v\tskip\n", table.Database, table.Name, utils.FormatBytes(table.TotalBytes), strings.Join(tableDisks, ",")); err != nil {
				apexLog.Warnf("can't write table list: %v", err)
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "%s.%s\t%s\t%v\t\n", table.Database, table.Name, utils.FormatBytes(table.TotalBytes), strings.Join(tableDisks, ",")); err != nil {
			apexLog.Warnf("can't write table list: %v", err)
		}
	}
	if err = w.Flush(); err != nil {
		apexLog.Warnf("can't flush table list: %v", err)
	}
	return nil
}
