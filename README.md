# goDiskTester
Simple application to easy the process of testing a whole gaggle of disks.

The program drops a pretty printed JSON log of tested disks so that subsequent runs are not duplicated.

The entire purpose of this program is to make it easy to watch a directory for a specific disk pattern and just swap disks in and out, generating a log of disk tests.

The program will take a directory to watch (`/dev/disk/by-id/` is a good choice) and a glob filter (like `scsi-STOSHIBA_*`) and then periodically scan for new disks.  When a new disk shows up it will kick it at the worker to run some contiguous and random I/O tests.  Disks that have already been tested will not be tested again.

## WARNING

This program will perform writes and reads on raw disk handles, MAKE SURE YOU SETUP GOOD FILTERS, or its going to blow away your disks.

*I AM NOT RESPONSIBLE FOR DATA LOSS*

## Usage
goDiskTester -db /tmp/history.db -root /dev/disk/by-id/ -filter='scsi-STOSHIBA_\*' -write-size=16

This will continually monitor `/dev/disk/by-id` for new drives that match the globbing pattern `scsi-STOSHIBA_*`, when a new disk is found it will write 8GB at the beginning and then 8GB at random offsets in 1MB blocks throughout the disk.  Writes are then read back and validated for errors.  On success a log of the test results is written to the file `/tmp/history.db`.
