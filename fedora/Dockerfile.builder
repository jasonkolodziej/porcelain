# =============================================================================
# STAGE 1: KERNEL MODULE BUILDER
# =============================================================================



ARG FEDORA_BOOTC_VERSION=43
FROM quay.io/fedora/fedora-bootc:${FEDORA_BOOTC_VERSION} AS builder

RUN set -eux; \
    kernel_version="$(rpm -q kernel-core --qf '%{VERSION}-%{RELEASE}.%{ARCH}\n' | sort -V | tail -1)"; \
    running_kernel="$(uname -r)"; \
    dnf install -y --setopt=install_weak_deps=False \
    "kernel-devel-${kernel_version}" \
    kernel-headers \
    gcc make git dkms elfutils-libelf-devel \
    && dnf clean all; \
    [ -e /lib/modules ] || ln -s /usr/lib/modules /lib/modules; \
    mkdir -p "/lib/modules/${kernel_version}"; \
    ln -sfn "/usr/src/kernels/${kernel_version}" "/lib/modules/${kernel_version}/build"; \
    mkdir -p "/lib/modules/${running_kernel}"; \
    ln -sfn "/usr/src/kernels/${kernel_version}" "/lib/modules/${running_kernel}/build"

WORKDIR /src

# RTL8126 driver (not yet in mainline)
RUN git clone --depth 1 https://github.com/awesometic/realtek-r8126-dkms.git && \
    cd realtek-r8126-dkms && \
    kver="$(rpm -q kernel-devel --qf '%{VERSION}-%{RELEASE}.%{ARCH}\n' | sort -V | tail -1)" && \
    make -C src KVER="$kver" && make -C src KVER="$kver" install DESTDIR=/out && \
    mkdir -p "/out/usr/lib/modules/${kver}/kernel/drivers/net" && \
    cp -av "/lib/modules/${kver}/kernel/drivers/net/r8126.ko" \
    "/out/usr/lib/modules/${kver}/kernel/drivers/net/r8126.ko"

#     # ITE IT87 driver
# RUN git clone --depth 1 https://github.com/frankcrawford/it87.git && \
#     cd it87 && make && make install DESTDIR=/out

# # AMD XDNA NPU driver
# RUN git clone --depth 1 https://github.com/amd/xdna-driver.git && \
#     cd xdna-driver/src && make && make install DESTDIR=/out


# =============================================================================
# STAGE 2: ZFS BUILDER
# =============================================================================
# FROM quay.io/fedora/fedora-bootc:${FEDORA_BOOTC_VERSION} AS zfs-builder

# RUN dnf install -y --setopt=install_weak_deps=False \
#     kernel-devel kernel-headers gcc make autoconf automake libtool \
#     libtirpc-devel libblkid-devel libuuid-devel libudev-devel openssl-devel \
#     zlib-devel libaio-devel libattr-devel elfutils-libelf-devel \
#     python3 python3-cffi python3-setuptools python3-packaging git wget \
#     && dnf clean all

# WORKDIR /src
# RUN wget https://github.com/openzfs/zfs/releases/download/zfs-2.2.4/zfs-2.2.4.tar.gz && \
#     tar -xzf zfs-2.2.4.tar.gz && cd zfs-2.2.4 && \
#     ./configure --prefix=/usr --with-config=kernel \
#     --with-linux=/usr/src/kernels/$(ls /usr/src/kernels/) \
#     --with-linux-obj=/usr/src/kernels/$(ls /usr/src/kernels/) && \
#     make -j$(nproc) && make install DESTDIR=/out/zfs && \
#     ./configure --prefix=/usr --with-config=user \
#     --with-udevdir=/usr/lib/udev \
#     --with-systemdunitdir=/usr/lib/systemd/system \
#     --with-systemdpresetdir=/usr/lib/systemd/system-preset \
#     --with-dracutdir=/usr/lib/dracut \
#     --with-mounthelperdir=/usr/sbin && \
#     make -j$(nproc) && make install DESTDIR=/out/zfs


FROM ghcr.io/aasseman/fcos-zfs:stable AS zfs-built

WORKDIR /zfs
