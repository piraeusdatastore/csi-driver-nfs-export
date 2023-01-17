#!/bin/bash
set -e

GANESHA_CLIENTS=${HOSTS_ALLOW:-'*'}

# Options for starting Ganesha
: ${GANESHA_LOGFILE:="/dev/stdout"}
: ${GANESHA_CONFIGFILE:="/etc/ganesha/ganesha.conf"}
: ${GANESHA_OPTIONS:="-N NIV_EVENT"} # NIV_DEBUG
: ${GANESHA_EPOCH:=""}
: ${GANESHA_EXPORT_ID:="77"}
: ${GANESHA_EXPORT:="/export"}
# : ${GANESHA_ACCESS:="*"}
# : ${GANESHA_ROOT_ACCESS:="*"}
# : ${GANESHA_NFS_PROTOCOLS:="3,4"}
# : ${GANESHA_TRANSPORTS:="UDP,TCP"}
: ${GANESHA_PSEUDO:="/"}

function bootstrap_config {
	echo "Bootstrapping Ganesha NFS config"
  cat <<END >${GANESHA_CONFIGFILE}

EXPORT
{
		# Export Id (mandatory, each EXPORT must have a unique Export_Id)
		Export_Id = ${GANESHA_EXPORT_ID};

		# Exported path (mandatory)
		Path = ${GANESHA_EXPORT};

		# Pseudo Path (for NFS v4)
		Pseudo = ${GANESHA_PSEUDO};

		# Access control options
		Access_Type = RW;
		Squash = none;

		# NFS protocol options
        Protocols = 3, 4;
        Transports = "UDP", "TCP";

		SecType = "sys";

		# Exporting FSAL
		FSAL {
			Name = VFS;
		}

		# Client Rule
		CLIENT {
			Clients = ${GANESHA_CLIENTS};
		}
}

NFS_Core_Param {
	fsid_device = true;
}

END
}

function bootstrap_export {
	if [ ! -f ${GANESHA_EXPORT} ]; then
		mkdir -p "${GANESHA_EXPORT}"
  fi
}

function init_rpc {
	echo "Starting rpcbind"
	rpcbind || return 0
	rpc.statd -L || return 0
	rpc.idmapd || return 0
	sleep 1
}

function init_dbus {
	echo "Starting dbus"
	rm -f /var/run/dbus/system_bus_socket
	rm -f /var/run/dbus/pid
	dbus-uuidgen --ensure
	dbus-daemon --system --fork
	sleep 1
}

function startup_script {
	if [ -f "${STARTUP_SCRIPT}" ]; then
  	/bin/sh ${STARTUP_SCRIPT}
	fi
}

bootstrap_config
bootstrap_export
startup_script

init_rpc
init_dbus


echo "Starting Ganesha NFS"
export LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib
exec /usr/bin/ganesha.nfsd -F -L ${GANESHA_LOGFILE} -f ${GANESHA_CONFIGFILE} ${GANESHA_OPTIONS}