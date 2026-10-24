<div align="center">
  <img src="web/assets/logo.png" alt="Anthology WebDAV Logo" width="130" />
  <h1>Anthology WebDAV</h1>
  <p><strong>Sıfır Disk Alanı Harcayan, Akıllı Kütüphane ve Çoklu Kaynak Çözücü Destekli Sanal WebDAV Medya Sunucusu</strong></p>

  <p>
    <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.24+" />
    <img src="https://img.shields.io/badge/Sağlayıcı-34_Aktif_Motor-0A84FF?style=for-the-badge" alt="34 Aktif Sağlayıcı" />
    <img src="https://img.shields.io/badge/Depolama-Sıfır_Disk_(In--Memory_VFS)-30D158?style=for-the-badge" alt="Sıfır Disk" />
    <img src="https://img.shields.io/badge/Protokol-WebDAV_%26_HLS/Range_Proxy-BF5AF2?style=for-the-badge" alt="WebDAV & Proxy" />
  </p>
</div>

---

> [!IMPORTANT]
> # 🚀 ASIL VE EN VERİMLİ KULLANIM HAKKINDA
>
> **Anthology-DAV sisteminin asıl ve en verimli kullanımı**, yerel bilgisayarınızı 7/24 açık tutmanıza veya yerel derleme yapmanıza gerek kalmadan doğrudan **kullanıcı hesabınızla** canlı sunucu üzerinden yararlanmaktır!
>
> 🌐 **Canlı Web Portalı & Hesap Yönetimi:** [https://anthology-dav.duckdns.org](https://anthology-dav.duckdns.org)
>
> 📚 **TÜM DETAYLI BİLGİLENDİRME VE EVRENSEL KULLANIM REHBERİ WIKI'DE:**  
> **Apple TV, iOS, Android TV, macOS, Windows** ortamlarında **Infuse, SenPlayer, VidHub, Kodi, Nova Player, VLC, Finder ve Windows Dosya Gezgini** ile sistemin nasıl bağlanıp kullanılacağını anlatan eksiksiz rehber GitHub Wiki sayfamızda yer almaktadır:  
> 
> 👉 **[https://github.com/falsisdev/anthology-dav/wiki](https://github.com/falsisdev/anthology-dav/wiki)** 👈
>
> *(Bu README dosyası yalnızca projeyi kendi yerel makinesinde veya sunucusunda sıfırdan derleyip çalıştırmak isteyen geliştiriciler için **Yerel Kurulum Rehberi** içermektedir.)*

---

## 💻 Yerel Kurulum Rehberi (Self-Hosted Local Setup)

Projeyi kendi bilgisayarınızda veya sunucunuzda bağımsız bir mikroservis olarak derlemek ve çalıştırmak için aşağıdaki adımları takip edin.

---

### 1. Ön Gereksinimler

Sisteminizde **Go (1.24 veya üzeri)**, **Git** ve isteğe bağlı olarak **Make** kurulu olmalıdır:

- **macOS (Homebrew):**
  ```bash
  brew install go git make
  ```
- **Windows (Winget):**
  ```powershell
  winget install GoLang.Go Git.Git
  ```
- **Linux (Ubuntu/Debian):**
  ```bash
  sudo apt update && sudo apt install -y golang git make
  ```

---

### 2. Projeyi İndirme ve Derleme

```bash
# Depoyu klonlayın
git clone https://github.com/falsisdev/anthology-dav.git
cd anthology-dav

# Make ile derleyin (macOS / Linux):
make build

# VEYA standart Go derleyicisi ile:
go build -o anthology-dav .
```

---

### 3. İzinlerin Yapılandırılması

Sunucunun kesintisiz çalışması ve yerel ağdaki diğer cihazlardan erişilebilmesi için gerekli izinleri verin:

#### A. Çalıştırma İzni ve Gatekeeper Temizliği (macOS / Linux)
```bash
chmod +x anthology-dav
# macOS Gatekeeper karantinasını kaldırmak için:
xattr -cr anthology-dav 2>/dev/null || true
```

#### B. Güvenlik Duvarı (Firewall) & Yerel Ağ
- **macOS:** Sunucuyu ilk kez çalıştırdığınızda gelen *"anthology-dav gelen ağ bağlantılarını kabul etsin mi?"* uyarısına **"İzin Ver"** deyin. Terminal uygulamanızın **Sistem Ayarları -> Gizlilik ve Güvenlik -> Yerel Ağ** izninin açık olduğundan emin olun.
- **Windows:** Güvenlik duvarı uyarı penceresinde **"Özel Ağlar"** seçeneğini işaretleyip **Erişime İzin Ver** butonuna tıklayın.

#### C. Yapılandırma Dosyaları Yazma İzni
```bash
touch library.json watchlist.json
chmod 644 library.json watchlist.json
```

---

### 4. Sunucuyu Çalıştırma & Parametreler

#### Hızlı Başlatma (Varsayılan Tek Kullanıcılı / Şifresiz Yerel Mod)
```bash
./anthology-dav
```
Sunucu varsayılan olarak `http://0.0.0.0:8085` adresinde başlar.

#### CLI Parametreleri ve Çevre Değişkenleri
| Parametre | Varsayılan | Açıklama |
|---|---|---|
| `-host` | `0.0.0.0` | Bağlanılacak ağ arayüzü (`127.0.0.1` sadece yerel; `0.0.0.0` tüm yerel ağ) |
| `-port` | `8085` | Çalışma portu (veya `PORT` çevre değişkeni) |
| `-public-url` | `""` | `.strm` dosyalarına yazılacak açık URL (Örn: `http://192.168.1.10:8085`) |
| `-multi-user` | `false` | SQLite tabanlı çok kullanıcılı giriş & kütüphane izolasyon modu |
| `-db` | `data/anthology.db` | SQLite veritabanı dosya yolu |
| `-library` | `library.json` | Tek kullanıcılı yerel kütüphane dosyası yolu |

#### Çok Kullanıcılı Modda Başlatma:
```bash
./anthology-dav -multi-user -db data/anthology.db -port 8085
```

---

### 5. Arka Plan Servisi Olarak Kurma (Otomatik Başlatma)

#### 🍎 macOS LaunchAgent (`make install-service`)
Proje içerisindeki otomatik servis yükleyici ile sistemi bilgisayarınız her açıldığında arka planda başlayacak şekilde kurabilirsiniz:
```bash
# Servisi kur ve başlat:
make install-service

# Canlı logları izle:
make logs

# Servisi durdur ve kaldır:
make uninstall-service
```

#### 🐧 Linux `systemd` Kullanıcı Servisi
1. `~/.config/systemd/user/anthology-dav.service` dosyasını oluşturun:
   ```ini
   [Unit]
   Description=Anthology WebDAV Server
   After=network-online.target

   [Service]
   WorkingDirectory=/tam/dosya/yolu/anthology-dav
   ExecStart=/tam/dosya/yolu/anthology-dav/anthology-dav -host 0.0.0.0 -port 8085
   Restart=always
   RestartSec=5

   [Install]
   WantedBy=default.target
   ```
2. Servisi aktif edip başlatın:
   ```bash
   systemctl --user daemon-reload
   systemctl --user enable --now anthology-dav.service
   ```

#### 🪟 Windows Başlangıç Servisi
`Win + R` tuşuna basıp `shell:startup` yazın. Açılan Başlangıç klasörüne `baslat.vbs` dosyası oluşturun:
```vbscript
Set WshShell = CreateObject("WScript.Shell")
WshShell.CurrentDirectory = "C:\anthology-dav"
WshShell.Run "C:\anthology-dav\anthology-dav.exe -host 0.0.0.0 -port 8085", 0, False
```

---

### 6. Yerel Uç Noktalar (Endpoints)

Sunucu yerelde çalışmaya başladığında şu adreslerden erişilebilir:

- 🖥️ **Web Paneli:** `http://localhost:8085/`
- 📁 **WebDAV Kök Dizini:** `http://localhost:8085/dav/`
  - `/dav/Filmler/`
  - `/dav/Diziler/`
  - `/dav/Animeler/`
- 📺 **Dinamik VLC Oynatma Listesi:** `http://localhost:8085/playlist.m3u`
- 🩺 **Sağlık & Sağlayıcı Durumu:** `http://localhost:8085/health`

---

### 7. Kayıtlı 34 VOD Sağlayıcı Motoru

Anthology-DAV içerisinde derlenmiş tüm yerleşik motorlar:

| Kategori | Adet | Sağlayıcılar |
|---|:---:|---|
| **⛩️ Anime** | **8** | `AnimeciX`, `Anizium`, `SonAnime`, `AniMom`, `Anizm`, `SeiCode`, `AsyaAnimeleri`, `AnimePrax` |
| **📺 Dizi** | **12** | `DiziWatch`, `DiziBox`, `DiziYou`, `DiziMom`, `DiziPod`, `DiziBak`, `Yabancı Dizi`, `DDizi`, `PuhuTV`, `TrDiziİzle`, `TvDiziler`, `Anthology Dizi Arşivi` |
| **🎬 Film** | **14** | `FilmModu`, `HDFilmCehennemi`, `HDFilmİzle`, `HDFilmDelisi`, `Kült Filmler`, `LiderFilmİzle`, `SetFilmİzle`, `SinemaCX`, `SineWix`, `VidLink Cloud`, `Vidmody`, `Webteİzle`, `YouTube Dizi & Film`, `Anthology M3U Arşivi` |

---

<div align="center">
  <p>Tüm cihazlar ve medya oynatıcılar için kapsamlı kurulum kılavuzuna <strong><a href="https://github.com/falsisdev/anthology-dav/wiki">Resmi Wiki Sayfasından</a></strong> ulaşabilirsiniz.</p>
</div>
