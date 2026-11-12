// Process leases for work that should have one scanner at a time.
// Row claims remain the real fence: a lease only stops every replica from
// scanning the same table on every tick. An expired holder is replaced.
package cluster

import (
	"database/sql"
	"os"
	"strconv"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

var processHolder = func() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "replica"
	}
	return host + ":" + strconv.Itoa(os.Getpid())
}()

// Acquire refreshes this process's lease or takes one that has expired.
// holder is stable for the process so a later tick extends the same lease.
func Acquire(db *sql.DB, name string, ttl time.Duration) (bool, error) {
	if db == nil || name == "" {
		return false, nil
	}
	now := time.Now().Unix()
	seconds := int64(ttl.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	until := now + seconds
	res, err := db.Exec(`UPDATE cluster_leases SET holder=?, lease_until=? WHERE name=? AND (lease_until<=? OR holder=?)`, processHolder, until, name, now, processHolder)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		return true, nil
	}
	_, err = db.Exec(`INSERT INTO cluster_leases(name, holder, lease_until) VALUES(?,?,?)`, name, processHolder, until)
	if err == nil {
		return true, nil
	}
	if !store.IsUniqueViolation(err) {
		return false, err
	}
	res, err = db.Exec(`UPDATE cluster_leases SET holder=?, lease_until=? WHERE name=? AND (lease_until<=? OR holder=?)`, processHolder, until, name, now, processHolder)
	if err != nil {
		return false, err
	}
	n, err = res.RowsAffected()
	return n == 1, err
}
