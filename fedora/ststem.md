# Key Hardware-Specific Optimizations

| Component | N5 Pro Spec | Containerfile Solution |
|-----------|-------------|----------------------|
| **CPU** | Ryzen AI 9 HX PRO 370 (Zen 5) | `amd-ucode-firmware`, `ryzenadj` |
| **GPU** | Radeon 890M (RDNA 3.5) | `amdgpu` driver, Mesa Vulkan, ROCm |
| **NPU** | 50 TOPS Ryzen AI | `amd-xdna-driver`, ONNX Runtime |
| **10GbE** | Marvell AQC113 | `kmod-aquantia`, `atlantic` driver |
| **5GbE** | Realtek RTL8126 | `kmod-r8126` |
| **USB4** | 40Gbps + DP Alt Mode | `thunderbolt-tools`, `bolt` |
| **OCuLink** | PCIe 4.0 x4 external | `pcieutils`, `egpu-switcher` |
| **Storage** | 5x SATA + 3x M.2/U.2 | `zfs`, `mdadm`, `smartmontools` |
| **ECC RAM** | DDR5 ECC SO-DIMM | `rasdaemon`, `edac-utils` |
| **EC Controller** | ITE 5571 | `it87` driver (force ID) |
| **Video** | AV1/H.265 4K120 | VAAPI, FFmpeg hwaccel |

## Known Limitations & Notes

1. **ITE 5571 EC Monitoring**: The N5 Pro uses an ITE 5571 embedded controller for fan/temperature monitoring that is **not fully exposed in Linux** . The `it87` driver is force-loaded with a generic ID, but full PWM control may require a BIOS update from Minisforum or community reverse-engineering.

2. **AQC113 10GbE**: The Marvell AQtion driver (`atlantic`) is included, but early N5 Pro units may need firmware updates via `fwupd` or manual flashing.

3. **NPU/XDNA**: The 50 TOPS NPU requires the `amd-xdna-driver` which is still maturing in upstream Linux. Some AI workloads may fallback to GPU compute.

4. **ECC Support**: ECC is enabled at the BIOS level. The `rasdaemon` service will log ECC errors to `journalctl`.

5. **Deepin 25 Authenticity**: This installs Fedora's packaged DDE components, not the full Deepin 25 "Solid" immutable system with deepin-wine, Linyaps, or Deepin's App Store .

6. **ZFS**: The ZFS kernel module is DKMS-based; ensure the bootc image is rebuilt after kernel updates.

Would you like me to add specific configurations for **OCuLink eGPU passthrough**, **USB4 networking**, or **ZFS pool auto-import** at boot?

Here's the **multistage Fedora bootc Containerfile** optimized for the Minisforum N5 Pro, separating the build environment from the final runtime image to reduce size and improve layer caching.

## Multistage Benefits

| Stage | Purpose | Size Impact |
|-------|---------|-------------|
| **builder** | Compiles `r8126`, `it87`, `xdna-driver` from source | ~2GB build tools **not** in final image |
| **zfs-builder** | Builds ZFS kernel module + userspace | ~1.5GB compiler toolchain **not** in final |
| **firmware-builder** | Downloads latest GPU/NIC firmware | Only firmware blobs copied (~50MB) |
| **runtime** | Final bootc image with artifacts copied | Clean, no dev tools |

**Estimated final image size**: ~3.5-4.5GB (vs ~6-8GB single-stage with build tools included)

## Key Multistage Optimizations

1. **No compiler toolchain in runtime** - `gcc`, `make`, `kernel-devel` stay in build stages
2. **Fresh firmware blobs** - Always pulls latest AMD GPU firmware for Radeon 890M
3. **DKMS-free ZFS** - Pre-built kernel module avoids runtime compilation
4. **Layer caching** - Firmware/build stages cache independently; runtime rebuilds faster on package updates only

Want me to add a **Stage 5 for custom DDE themes** or **Stage 6 for pre-configured ZFS pools**?

Yes, absolutely! You can split the Deepin Desktop Environment into its own **optional extension Dockerfile** that builds on top of the base N5 Pro hardware image. This is a common pattern in bootc/container workflows—keeping the base image lean and adding DE layers as needed.

## **Why DNF/OSTree Firmware is Better Than Manual Fetching**

| Aspect | Manual Fetching (old way) | DNF `linux-firmware` (recommended) |
|--------|---------------------------|-----------------------------------|
| **Update mechanism** | Static blobs, never updates | Atomic OSTree upgrades via `bootc upgrade`  |
| **Rollback** | No rollback capability | Full rollback to previous firmware version  |
| **Delta updates** | Full re-download | OSTree file-level deduplication  |
| **Tracking** | Untracked files | RPM database in `/usr/share/rpm`  |
| **Security** | Manual verification | GPG-signed RPMs from Fedora repos  |
| **Bootc lint** | May fail (`bootc container lint`) | Clean OSTree-native compliance |

The `linux-firmware` package in Fedora is actively maintained and includes firmware for AMD GPUs (Radeon 890M), Marvell AQC113, Realtek RTL8126, and more . On bootc/OSTree systems, firmware updates are delivered atomically as part of image updates—if a firmware breaks something, you simply `bootc rollback` .

---

## **Build Commands**

```bash
# Build base (headless, includes all firmware via DNF)
sudo podman build \
    --tag n5-pro-base:latest \
    -f Containerfile.base

# Build Deepin extension (inherits base firmware, adds DE)
sudo podman build \
    --tag n5-pro-deepin:latest \
    -f Containerfile.deepin

# Generate ISO for base
mkdir -p ./output-base
sudo podman run \
    --rm -it --privileged --pull=newer \
    --security-opt label=type:unconfined_t \
    -v ./output-base:/output \
    -v /var/lib/containers/storage:/var/lib/containers/storage \
    quay.io/centos-bootc/bootc-image-builder:latest \
    --type iso --rootfs btrfs --local n5-pro-base:latest

podman run --rm -it --privileged --pull=newer \
  --security-opt label=type:unconfined_t \
  -v "$PWD/output-iso:/output" \
  -v "$GRAPHROOT:/var/lib/containers/storage" \
  quay.io/centos-bootc/bootc-image-builder:latest \
  --type iso \
  --rootfs btrfs \
  --local ghcr.io/jasonkolodziej/porcelain-base:latest

# Generate ISO for Deepin
mkdir -p ./output-deepin
sudo podman run \
    --rm -it --privileged --pull=newer \
    --security-opt label=type:unconfined_t \
    -v ./output-deepin:/output \
    -v /var/lib/containers/storage:/var/lib/containers/storage \
    quay.io/centos-bootc/bootc-image-builder:latest \
    --type iso --rootfs btrfs --local n5-pro-deepin:latest
```

---

## **Runtime Firmware Updates**

Even after deployment, firmware can be updated via **LVFS** (Linux Vendor Firmware Service) using `fwupd`, which is included in both images:

```bash
# On a running N5 Pro system
fwupdmgr refresh          # Check for firmware updates
fwupdmgr get-updates      # List available updates
fwupdmgr update           # Apply BIOS/UEFI/device firmware
```

This handles motherboard BIOS, SSD firmware, and Thunderbolt controller updates independently of the OSTree image .

---

## **Architecture Summary**

```
quay.io/fedora/fedora-bootc:42
        │
        ├──► builder stage (kmods: r8126, it87, xdna)
        ├──► zfs-builder stage (ZFS kernel module)
        │
        ▼
┌─────────────────────┐
│  n5-pro-base:latest │◄── DNF provides linux-firmware
│  (hardware + OSTree │    + all GPU/NIC/EC firmware
│   firmware tracking)│
└─────────────────────┘
        │
        ├──► Containerfile.kde ──► n5-pro-kde:latest
        ├──► Containerfile.gnome ──► n5-pro-gnome:latest
        └──► Containerfile.deepin ──► n5-pro-deepin:latest
```

The firmware is now **properly OSTree-managed** through DNF, giving you atomic updates, rollback capability, and no untracked files in `/usr/lib/firmware`.


No — if you want the installed system to **update from GHCR**, you should **not** use `--local` during ISO generation. Instead, reference the **GHCR image directly** in the `bootc-image-builder` command.

Here's the distinction:

---

## `--local` vs Registry Reference

| Flag | Behavior | Update Source After Install |
|------|----------|---------------------------|
| `--local` | Uses image from local Podman storage only | **None** — system has no registry reference |
| `--target-containers-storage` (default without `--local`) | Pulls from registry, embeds, and sets registry as source | **GHCR** — `bootc upgrade` queries GHCR |

---

## Correct Command for GHCR-Updatable ISO

```bash
# 1. Build locally
podman build --tag ghcr.io/youruser/n5-pro-base:latest -f Containerfile.base

# 2. Push to GHCR (so BIB can pull it + system knows the source)
podman push ghcr.io/youruser/n5-pro-base:latest

# 3. Generate ISO — reference GHCR directly, NO --local
# Use Podman's real graph root (works on mac podman machine too)
GRAPHROOT="$(podman info --format '{{.Store.GraphRoot}}')"
mkdir -p ./output-iso
podman run \
    --rm -it --privileged --pull=newer \
    --security-opt label=type:unconfined_t \
    -v "$PWD/output-iso:/output" \
    -v "$GRAPHROOT:/var/lib/containers/storage" \
    quay.io/centos-bootc/bootc-image-builder:latest \
    build \
    --type iso \
    --target-arch x86_64 \
    --rootfs btrfs \
    ghcr.io/jasonkolodziej/porcelain-base:latest

    # Flash to USB (macOS)
    diskutil list # Identify your USB drive (e.g., /dev/diskN)
    diskutil eraseDisk fat32 N5ProInstaller /dev/diskN; sync
diskutil unmountDisk /dev/diskN; sync
sudo dd if=$PWD/output-iso/install.iso of=/dev/rdiskN bs=4m status=progress; sync
diskutil eject /dev/diskN
```

**Option 1: Boot the ISO and interrupt anaconda for interactive disk selection**

Here's the practical workflow:

1. **Flash the ISO to USB** (on macOS):
   ```bash
   diskutil list  # Find your USB device (e.g., /dev/disk4)
   diskutil unmountDisk /dev/disk4
   dd if=output-iso/disk.iso of=/dev/rdisk4 bs=1m
   diskutil eject /dev/disk4
   ```

2. **Boot N5 Pro from USB** → BIOS/UEFI menu, select USB device

3. **At the anaconda boot screen**, you have options:
   - **Press Tab** (or Esc depending on GRUB version) to edit kernel parameters
   - Add `inst.interactive` to force interactive mode
   - Or just **press Escape** when anaconda starts to get a manual partitioning UI

4. **Anaconda will show disk selection** → Choose your target disk (e.g., `/dev/sda` vs `/dev/sdb`)

5. **Proceed with installation** → Completes automatically from there


This is complex. **For N5 Pro with multiple disks, just use the interactive Escape/Tab method at boot** — it's the simplest path and gives you full control without rebuilding the ISO.

> **Note**: Removed `--local` and the containers-storage volume mount. BIB now pulls from GHCR directly.

---

## What Gets Embedded

The ISO now contains:
- The **image layers** from GHCR (same as local, but verified)
- **Image reference metadata** pointing to `ghcr.io/youruser/n5-pro-base:latest`

After install, `bootc status` shows:
```bash
sudo bootc status
# Current booted image: ghcr.io/youruser/n5-pro-base:latest
# Image source: registry (ghcr.io)
```

Future updates:
```bash
sudo bootc upgrade
# Automatically queries GHCR for newer digests
```

---

## If You Already Built With `--local`

The installed system has no update source. Fix it post-install:

```bash
# On the N5 Pro after first boot
sudo bootc switch ghcr.io/youruser/n5-pro-base:latest
# Reboots into same image but now tracked from GHCR

# Then updates work normally
sudo bootc upgrade
```

---

## Summary Table

| Scenario | Build Command | Post-Install Update |
|----------|-------------|-------------------|
| **Local-only ISO** (airgapped) | `--local` + `localhost/...` | Manual `bootc switch` or new ISO |
| **GHCR-tracked ISO** (recommended) | No `--local`, use `ghcr.io/...` | `sudo bootc upgrade` |
| **Switch source later** | N/A | `sudo bootc switch ghcr.io/...` |

Use `--local` only for **offline/airgapped** installs where the N5 Pro will never have registry access. For normal use, always reference the registry URL directly so `bootc upgrade` works out of the box.


Thanks, this helps. I’m going to wire a clean “download releases, then install/copy artifacts” flow by fixing the builder stage output and aligning the base file to consume it reliably.

Read [](file:///Users/jasonkolodziej/Code/porcelain/fedora/Dockerfile.base)

Read [](file:///Users/jasonkolodziej/Code/porcelain/fedora/Dockerfile.builder)

Read [](file:///Users/jasonkolodziej/Code/porcelain/fedora)

I checked your context and the key issue is that the added `.deb` asset won’t install natively in Fedora bootc. I’ll quickly inspect release assets for these drivers to confirm whether prebuilt `.rpm` artifacts exist.

Ran terminal command:  for repo in awesometic/realtek-r8126-dkms frankcrawford/it87 amd/xdna-driver; do echo "== $repo =="; curl -fsSL "https://api.github.com/repos/$repo/releases?per_page=5" | rg '"name"|"browser_download_url"' | head -n 80; echo; done

I verified release assets: only Realtek has a Debian DKMS package, and the others don’t publish installable release binaries. I’ll do one quick runtime check to see which modules are already in Fedora’s stock kernel modules.

Ran terminal command:  podman run --rm quay.io/fedora/fedora-bootc:43 bash -lc 'dnf -y install --setopt=install_weak_deps=False kernel-modules-extra >/dev/null && kver=$(rpm -q kernel-core --qf "%{VERSION}-%{RELEASE}.%{ARCH}\n" | tail -1) && echo "kernel=$kver" && for m in r8126 it87 amdxdna atlantic; do if find "/usr/lib/modules/$kver" -type f -name "$m.ko*" | head -1 | grep -q .; then echo "$m: present"; else echo "$m: missing"; fi; done'

Yes, but only partially for your current driver set.

What your checks show on Fedora 43 x86_64:
- `it87`: present in stock `kernel-modules-extra`
- `amdxdna`: present in stock `kernel-modules-extra`
- `atlantic`: present in stock `kernel-modules-extra`
- `r8126`: missing from stock Fedora kernel modules

Release availability:
- `awesometic/realtek-r8126-dkms` publishes a `.deb` DKMS package, not a Fedora `.rpm`
- `frankcrawford/it87` and `amd/xdna-driver` do not provide prebuilt Fedora release binaries you can just install

So the practical answer is:
- You can avoid custom builds for `it87`, `amdxdna`, and `atlantic` by relying on Fedora kernel modules.
- You cannot cleanly avoid a build for `r8126` unless you find a trusted prebuilt Fedora RPM source for your exact kernel.

Also, your current failure is from this mismatch:
- base image build expects `/out/usr/lib/modules` from a `builder` stage
- but that output is not guaranteed in the file you are building, so `COPY --from=builder ...` fails

If you want, I can implement a clean split next:
1. Keep base using stock modules only.
2. Build/install only `r8126` in a dedicated optional stage/image.
3. Make the base build not hard-fail when that custom stage is absent.
