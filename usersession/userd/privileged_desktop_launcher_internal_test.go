// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package userd_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/userd"
)

type privilegedDesktopLauncherInternalSuite struct {
	testutil.BaseTest
}

var _ = Suite(&privilegedDesktopLauncherInternalSuite{})

var mockFileSystem = []string{
	"/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop",
	"/var/lib/snapd/desktop/applications/multipass_multipass-gui.desktop",
	"/var/lib/snapd/desktop/applications/cevelop_cevelop.desktop",
	"/var/lib/snapd/desktop/applications/egmde-confined-desktop_egmde-confined-desktop.desktop",
	"/var/lib/snapd/desktop/applications/classic-snap-analyzer_classic-snap-analyzer.desktop",
	"/var/lib/snapd/desktop/applications/vlc_vlc.desktop",
	"/var/lib/snapd/desktop/applications/gnome-calculator_gnome-calculator.desktop",
	"/var/lib/snapd/desktop/applications/mir-kiosk-kodi_mir-kiosk-kodi.desktop",
	"/var/lib/snapd/desktop/applications/gnome-characters_gnome-characters.desktop",
	"/var/lib/snapd/desktop/applications/clion_clion.desktop",
	"/var/lib/snapd/desktop/applications/gnome-system-monitor_gnome-system-monitor.desktop",
	"/var/lib/snapd/desktop/applications/inkscape_inkscape.desktop",
	"/var/lib/snapd/desktop/applications/gnome-logs_gnome-logs.desktop",
	"/var/lib/snapd/desktop/applications/foo-bar/baz.desktop",
	"/var/lib/snapd/desktop/applications/baz/foo-bar.desktop",

	// A desktop file ID provided by a snap may be shadowed by the
	// host system.
	"/usr/share/applications/shadow-test.desktop",
	"/var/lib/snapd/desktop/applications/shadow-test.desktop",
}

var chromiumDesktopFile = `[Desktop Entry]
X-SnapInstanceName=chromium
Version=1.0
Name=Chromium Web Browser
Name[ast]=Restolador web Chromium
Name[bg]=Уеб четец Chromium
Name[bn]=ক্রোমিয়াম ওয়েব ব্রাউজার
Name[bs]=Chromium web preglednik
Name[ca]=Navegador web Chromium
Name[ca@valencia]=Navegador web Chromium
Name[da]=Chromium netbrowser
Name[de]=Chromium-Webbrowser
Name[en_AU]=Chromium Web Browser
Name[eo]=Kromiumo retfoliumilo
Name[es]=Navegador web Chromium
Name[et]=Chromiumi veebibrauser
Name[eu]=Chromium web-nabigatzailea
Name[fi]=Chromium-selain
Name[fr]=Navigateur Web Chromium
Name[gl]=Navegador web Chromium
Name[he]=דפדפן האינטרנט כרומיום
Name[hr]=Chromium web preglednik
Name[hu]=Chromium webböngésző
Name[hy]=Chromium ոստայն զննարկիչ
Name[ia]=Navigator del web Chromium
Name[id]=Peramban Web Chromium
Name[it]=Browser web Chromium
Name[ja]=Chromium ウェブ・ブラウザ
Name[ka]=ვებ ბრაუზერი Chromium
Name[ko]=Chromium 웹 브라우저
Name[kw]=Peurel wias Chromium
Name[ms]=Pelayar Web Chromium
Name[nb]=Chromium nettleser
Name[nl]=Chromium webbrowser
Name[pt_BR]=Navegador de Internet Chromium
Name[ro]=Navigator Internet Chromium
Name[ru]=Веб-браузер Chromium
Name[sl]=Chromium spletni brskalnik
Name[sv]=Webbläsaren Chromium
Name[ug]=Chromium توركۆرگۈ
Name[vi]=Trình duyệt Web Chromium
Name[zh_CN]=Chromium 网页浏览器
Name[zh_HK]=Chromium 網頁瀏覽器
Name[zh_TW]=Chromium 網頁瀏覽器
GenericName=Web Browser
GenericName[ar]=متصفح الشبكة
GenericName[ast]=Restolador web
GenericName[bg]=Уеб браузър
GenericName[bn]=ওয়েব ব্রাউজার
GenericName[bs]=Web preglednik
GenericName[ca]=Navegador web
GenericName[ca@valencia]=Navegador web
GenericName[cs]=WWW prohlížeč
GenericName[da]=Browser
GenericName[de]=Web-Browser
GenericName[el]=Περιηγητής ιστού
GenericName[en_AU]=Web Browser
GenericName[en_GB]=Web Browser
GenericName[eo]=Retfoliumilo
GenericName[es]=Navegador web
GenericName[et]=Veebibrauser
GenericName[eu]=Web-nabigatzailea
GenericName[fi]=WWW-selain
GenericName[fil]=Web Browser
GenericName[fr]=Navigateur Web
GenericName[gl]=Navegador web
GenericName[gu]=વેબ બ્રાઉઝર
GenericName[he]=דפדפן אינטרנט
GenericName[hi]=वेब ब्राउज़र
GenericName[hr]=Web preglednik
GenericName[hu]=Webböngésző
GenericName[hy]=Ոստայն զննարկիչ
GenericName[ia]=Navigator del Web
GenericName[id]=Peramban Web
GenericName[it]=Browser web
GenericName[ja]=ウェブ・ブラウザ
GenericName[ka]=ვებ ბრაუზერი
GenericName[kn]=ಜಾಲ ವೀಕ್ಷಕ
GenericName[ko]=웹 브라우저
GenericName[kw]=Peurel wias
GenericName[lt]=Žiniatinklio naršyklė
GenericName[lv]=Tīmekļa pārlūks
GenericName[ml]=വെബ് ബ്രൌസര്‍
GenericName[mr]=वेब ब्राऊजर
GenericName[ms]=Pelayar Web
GenericName[nb]=Nettleser
GenericName[nl]=Webbrowser
GenericName[or]=ଓ୍ବେବ ବ୍ରାଉଜର
GenericName[pl]=Przeglądarka WWW
GenericName[pt]=Navegador Web
GenericName[pt_BR]=Navegador web
GenericName[ro]=Navigator de Internet
GenericName[ru]=Веб-браузер
GenericName[sk]=WWW prehliadač
GenericName[sl]=Spletni brskalnik
GenericName[sr]=Интернет прегледник
GenericName[sv]=Webbläsare
GenericName[ta]=இணைய உலாவி
GenericName[te]=మహాతల అన్వేషి
GenericName[th]=เว็บเบราว์เซอร์
GenericName[tr]=Web Tarayıcı
GenericName[ug]=توركۆرگۈ
GenericName[uk]=Навігатор Тенет
GenericName[vi]=Bộ duyệt Web
GenericName[zh_CN]=网页浏览器
GenericName[zh_HK]=網頁瀏覽器
GenericName[zh_TW]=網頁瀏覽器
Comment=Access the Internet
Comment[ar]=الدخول إلى الإنترنت
Comment[ast]=Accesu a Internet
Comment[bg]=Достъп до интернет
Comment[bn]=ইন্টারনেটে প্রবেশ করুন
Comment[bs]=Pristup internetu
Comment[ca]=Accediu a Internet
Comment[ca@valencia]=Accediu a Internet
Comment[cs]=Přístup k internetu
Comment[da]=Få adgang til internettet
Comment[de]=Internetzugriff
Comment[el]=Πρόσβαση στο Διαδίκτυο
Comment[en_AU]=Access the Internet
Comment[en_GB]=Access the Internet
Comment[eo]=Akiri interreton
Comment[es]=Acceda a Internet
Comment[et]=Pääs Internetti
Comment[eu]=Sartu Internetera
Comment[fi]=Käytä internetiä
Comment[fil]=I-access ang Internet
Comment[fr]=Accéder à Internet
Comment[gl]=Acceda a Internet
Comment[gu]=ઇંટરનેટ ઍક્સેસ કરો
Comment[he]=גישה לאינטרנט
Comment[hi]=इंटरनेट तक पहुंच स्थापित करें
Comment[hr]=Pristupite Internetu
Comment[hu]=Az internet elérése
Comment[hy]=Մուտք համացանց
Comment[ia]=Accede a le Interrete
Comment[id]=Akses Internet
Comment[it]=Accesso a Internet
Comment[ja]=インターネットにアクセス
Comment[ka]=ინტერნეტში შესვლა
Comment[kn]=ಇಂಟರ್ನೆಟ್ ಅನ್ನು ಪ್ರವೇಶಿಸಿ
Comment[ko]=인터넷에 연결합니다
Comment[kw]=Hedhes an Kesrosweyth
Comment[lt]=Interneto prieiga
Comment[lv]=Piekļūt internetam
Comment[ml]=ഇന്റര്‍‌നെറ്റ് ആക്‌സസ് ചെയ്യുക
Comment[mr]=इंटरनेटमध्ये प्रवेश करा
Comment[ms]=Mengakses Internet
Comment[nb]=Bruk internett
Comment[nl]=Verbinding maken met internet
Comment[or]=ଇଣ୍ଟର୍ନେଟ୍ ପ୍ରବେଶ କରନ୍ତୁ
Comment[pl]=Skorzystaj z internetu
Comment[pt]=Aceder à Internet
Comment[pt_BR]=Acessar a internet
Comment[ro]=Accesați Internetul
Comment[ru]=Доступ в Интернет
Comment[sk]=Prístup do siete Internet
Comment[sl]=Dostop do interneta
Comment[sr]=Приступите Интернету
Comment[sv]=Surfa på Internet
Comment[ta]=இணையத்தை அணுகுதல்
Comment[te]=ఇంటర్నెట్‌ను ఆక్సెస్ చెయ్యండి
Comment[th]=เข้าถึงอินเทอร์เน็ต
Comment[tr]=İnternet'e erişin
Comment[ug]=ئىنتېرنېت زىيارىتى
Comment[uk]=Доступ до Інтернету
Comment[vi]=Truy cập Internet
Comment[zh_CN]=访问互联网
Comment[zh_HK]=連線到網際網路
Comment[zh_TW]=連線到網際網路
Exec=env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop /snap/bin/chromium %U
Terminal=false
Type=Application
Icon=/snap/chromium/1193/chromium.png
Categories=Network;WebBrowser;
MimeType=text/html;text/xml;application/xhtml_xml;x-scheme-handler/http;x-scheme-handler/https;
StartupNotify=true
StartupWMClass=chromium
Actions=NewWindow;Incognito;TempProfile;

[Desktop Action NewWindow]
Name=Open a New Window
Name[ast]=Abrir una Ventana Nueva
Name[bg]=Отваряне на Нов прозорец
Name[bn]=একটি নতুন উইন্ডো খুলুন
Name[bs]=Otvori novi prozor
Name[ca]=Obre una finestra nova
Name[ca@valencia]=Obri una finestra nova
Name[da]=Åbn et nyt vindue
Name[de]=Ein neues Fenster öffnen
Name[en_AU]=Open a New Window
Name[eo]=Malfermi novan fenestron
Name[es]=Abrir una ventana nueva
Name[et]=Ava uus aken
Name[eu]=Ireki leiho berria
Name[fi]=Avaa uusi ikkuna
Name[fr]=Ouvrir une nouvelle fenêtre
Name[gl]=Abrir unha nova xanela
Name[he]=פתיחת חלון חדש
Name[hy]=Բացել նոր պատուհան
Name[ia]=Aperi un nove fenestra
Name[it]=Apri una nuova finestra
Name[ja]=新しいウィンドウを開く
Name[ka]=ახალი ფანჯრის გახსნა
Name[kw]=Egery fenester noweth
Name[ms]=Buka Tetingkap Baru
Name[nb]=Åpne et nytt vindu
Name[nl]=Nieuw venster openen
Name[pt_BR]=Abre uma nova janela
Name[ro]=Deschide o fereastră nouă
Name[ru]=Открыть новое окно
Name[sl]=Odpri novo okno
Name[sv]=Öppna ett nytt fönster
Name[ug]=يېڭى كۆزنەك ئاچ
Name[uk]=Відкрити нове вікно
Name[vi]=Mở cửa sổ mới
Name[zh_CN]=打开新窗口
Name[zh_TW]=開啟新視窗
Exec=env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop /snap/bin/chromium

[Desktop Action Incognito]
Name=Open a New Window in incognito mode
Name[ast]=Abrir una ventana nueva en mou incógnitu
Name[bg]=Отваряне на нов прозорец в режим \"инкогнито\"
Name[bn]=একটি নতুন উইন্ডো খুলুন ইনকোগনিটো অবস্থায়
Name[bs]=Otvori novi prozor u privatnom modu
Name[ca]=Obre una finestra nova en mode d'incògnit
Name[ca@valencia]=Obri una finestra nova en mode d'incògnit
Name[de]=Ein neues Fenster im Inkognito-Modus öffnen
Name[en_AU]=Open a New Window in incognito mode
Name[eo]=Malfermi novan fenestron nekoniĝeble
Name[es]=Abrir una ventana nueva en modo incógnito
Name[et]=Ava uus aken tundmatus olekus
Name[eu]=Ireki leiho berria isileko moduan
Name[fi]=Avaa uusi ikkuna incognito-tilassa
Name[fr]=Ouvrir une nouvelle fenêtre en mode navigation privée
Name[gl]=Abrir unha nova xanela en modo de incógnito
Name[he]=פתיחת חלון חדש במצב גלישה בסתר
Name[hy]=Բացել նոր պատուհան ծպտյալ աշխատակերպում
Name[ia]=Aperi un nove fenestra in modo incognite
Name[it]=Apri una nuova finestra in modalità incognito
Name[ja]=新しいシークレット ウィンドウを開く
Name[ka]=ახალი ფანჯრის ინკოგნიტოდ გახსნა
Name[kw]=Egry fenester noweth en modh privedh
Name[ms]=Buka Tetingkap Baru dalam mod menyamar
Name[nl]=Nieuw venster openen in incognito-modus
Name[pt_BR]=Abrir uma nova janela em modo anônimo
Name[ro]=Deschide o fereastră nouă în mod incognito
Name[ru]=Открыть новое окно в режиме инкогнито
Name[sl]=Odpri novo okno v načinu brez beleženja
Name[sv]=Öppna ett nytt inkognitofönster
Name[ug]=يوشۇرۇن ھالەتتە يېڭى كۆزنەك ئاچ
Name[uk]=Відкрити нове вікно у приватному режимі
Name[vi]=Mở cửa sổ mới trong chế độ ẩn danh
Name[zh_CN]=以隐身模式打开新窗口
Name[zh_TW]=以匿名模式開啟新視窗
Exec=env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop /snap/bin/chromium --incognito

[Desktop Action TempProfile]
Name=Open a New Window with a temporary profile
Name[ast]=Abrir una ventana nueva con perfil temporal
Name[bg]=Отваряне на Нов прозорец с временен профил
Name[bn]=সাময়িক প্রোফাইল সহ একটি নতুন উইন্ডো খুলুন
Name[bs]=Otvori novi prozor pomoću privremenog profila
Name[ca]=Obre una finestra nova amb un perfil temporal
Name[ca@valencia]=Obri una finestra nova amb un perfil temporal
Name[de]=Ein neues Fenster mit einem temporären Profil öffnen
Name[en_AU]=Open a New Window with a temporary profile
Name[eo]=Malfermi novan fenestron portempe
Name[es]=Abrir una ventana nueva con perfil temporal
Name[et]=Ava uus aken ajutise profiiliga
Name[eu]=Ireki leiho berria behin-behineko profil batekin
Name[fi]=Avaa uusi ikkuna käyttäen väliaikaista profiilia
Name[fr]=Ouvrir une nouvelle fenêtre avec un profil temporaire
Name[gl]=Abrir unha nova xanela con perfil temporal
Name[he]=פתיחת חלון חדש עם פרופיל זמני
Name[hy]=Բացել նոր պատուհան ժամանակավոր հատկագրով
Name[ia]=Aperi un nove fenestra con un profilo provisori
Name[it]=Apri una nuova finestra con un profilo temporaneo
Name[ja]=一時プロファイルで新しいウィンドウを開く
Name[ka]=ახალი ფანჯრის გახსნა დროებით პროფილში
Name[kw]=Egery fenester noweth gen profil dres prys
Name[ms]=Buka Tetingkap Baru dengan profil sementara
Name[nb]=Åpne et nytt vindu med en midlertidig profil
Name[nl]=Nieuw venster openen met een tijdelijk profiel
Name[pt_BR]=Abrir uma nova janela com um perfil temporário
Name[ro]=Deschide o fereastră nouă cu un profil temporar
Name[ru]=Открыть новое окно с временным профилем
Name[sl]=Odpri novo okno z začasnim profilom
Name[sv]=Öppna ett nytt fönster med temporär profil
Name[ug]=ۋاقىتلىق سەپلىمە ھۆججەت بىلەن يېڭى كۆزنەك ئاچ
Name[vi]=Mở cửa sổ mới với hồ sơ tạm
Name[zh_CN]=以临时配置文件打开新窗口
Name[zh_TW]=以暫時性個人身分開啟新視窗
Exec=env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop /snap/bin/chromium --temp-profile
`

func existsOnMockFileSystem(desktop_file string) (bool, bool, error) {
	existsOnMockFileSystem := strutil.ListContains(mockFileSystem, desktop_file)
	return existsOnMockFileSystem, existsOnMockFileSystem, nil
}

func (s *privilegedDesktopLauncherInternalSuite) mockEnv(key, value string) {
	old := os.Getenv(key)
	os.Setenv(key, value)
	s.AddCleanup(func() {
		os.Setenv(key, old)
	})
}

func (s *privilegedDesktopLauncherInternalSuite) TestDesktopFileSearchPath(c *C) {
	s.mockEnv("HOME", "/home/user")
	s.mockEnv("XDG_DATA_HOME", "")
	s.mockEnv("XDG_DATA_DIRS", "")

	// Default search path
	c.Check(userd.DesktopFileSearchPath(), DeepEquals, []string{
		"/home/user/.local/share/applications",
		"/usr/local/share/applications",
		"/usr/share/applications",
	})

	// XDG_DATA_HOME will override the first path
	s.mockEnv("XDG_DATA_HOME", "/home/user/share")
	c.Check(userd.DesktopFileSearchPath(), DeepEquals, []string{
		"/home/user/share/applications",
		"/usr/local/share/applications",
		"/usr/share/applications",
	})

	// XDG_DATA_DIRS changes the remaining paths
	s.mockEnv("XDG_DATA_DIRS", "/usr/share:/var/lib/snapd/desktop")
	c.Check(userd.DesktopFileSearchPath(), DeepEquals, []string{
		"/home/user/share/applications",
		"/usr/share/applications",
		"/var/lib/snapd/desktop/applications",
	})
}

func (s *privilegedDesktopLauncherInternalSuite) TestDesktopFileIDToFilenameSucceedsWithValidId(c *C) {
	restore := userd.MockRegularFileExists(existsOnMockFileSystem)
	defer restore()
	s.mockEnv("XDG_DATA_DIRS", "/usr/local/share:/usr/share:/var/lib/snapd/desktop")

	var desktopIdTests = []struct {
		id     string
		expect string
	}{
		{"mir-kiosk-scummvm_mir-kiosk-scummvm.desktop", "/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop"},
		{"foo-bar-baz.desktop", "/var/lib/snapd/desktop/applications/foo-bar/baz.desktop"},
		{"baz-foo-bar.desktop", "/var/lib/snapd/desktop/applications/baz/foo-bar.desktop"},
		{"shadow-test.desktop", "/usr/share/applications/shadow-test.desktop"},
	}

	for _, test := range desktopIdTests {
		actual, err := userd.DesktopFileIDToFilename(test.id)
		c.Assert(err, IsNil)
		c.Assert(actual, Equals, test.expect)
	}
}

func (s *privilegedDesktopLauncherInternalSuite) TestDesktopFileIDToFilenameFailsWithInvalidId(c *C) {
	restore := userd.MockRegularFileExists(existsOnMockFileSystem)
	defer restore()
	s.mockEnv("XDG_DATA_DIRS", "/usr/local/share:/usr/share:/var/lib/snapd/desktop")

	var desktopIdTests = []string{
		"mir-kiosk-scummvm-mir-kiosk-scummvm.desktop",
		"bar-foo-baz.desktop",
		"bar-baz-foo.desktop",
		"foo-bar_foo-bar.desktop",
		// special path segments cannot be smuggled inside desktop IDs
		"bar-..-vlc_vlc.desktop",
		"foo-bar/baz.desktop",
		"bar/../vlc_vlc.desktop",
		"../applications/vlc_vlc.desktop",
		// Other invalid desktop IDs
		"---------foo-bar-baz.desktop",
		"foo-bar-baz.desktop-foo-bar",
		"not-a-dot-desktop",
		"以临时配置文件打开新-non-ascii-here-too.desktop",
	}

	for _, id := range desktopIdTests {
		_, err := userd.DesktopFileIDToFilename(id)
		c.Check(err, ErrorMatches, `cannot find desktop file for ".*"`, Commentf(id))
	}
}

func (s *privilegedDesktopLauncherInternalSuite) TestVerifyDesktopFileLocation(c *C) {
	restore := userd.MockRegularFileExists(existsOnMockFileSystem)
	defer restore()
	s.mockEnv("XDG_DATA_DIRS", "/usr/local/share:/usr/share:/var/lib/snapd/desktop")

	// Resolved desktop files belonging to snaps will pass verification:
	filename, err := userd.DesktopFileIDToFilename("mir-kiosk-scummvm_mir-kiosk-scummvm.desktop")
	c.Assert(err, IsNil)
	err = userd.VerifyDesktopFileLocation(filename)
	c.Check(err, IsNil)

	// Desktop IDs belonging to host system apps fail:
	filename, err = userd.DesktopFileIDToFilename("shadow-test.desktop")
	c.Assert(err, IsNil)
	err = userd.VerifyDesktopFileLocation(filename)
	c.Check(err, ErrorMatches, "only launching snap applications from /var/lib/snapd/desktop/applications is supported")
}

func (s *privilegedDesktopLauncherInternalSuite) TestParseExecCommandSucceedsWithValidEntry(c *C) {
	var testCases = []struct {
		cmd    string
		expect []string
	}{
		// valid with no exec variables
		{"env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop /snap/bin/mir-kiosk-scummvm %U",
			[]string{"env", "BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop", "/snap/bin/mir-kiosk-scummvm"}},
		// valid with literal '%' and no exec variables
		{"/snap/bin/foo -f %%bar", []string{"/snap/bin/foo", "-f", "%bar"}},
		{"/snap/bin/foo -f %%bar %%baz", []string{"/snap/bin/foo", "-f", "%bar", "%baz"}},
		// valid where quoted strings are passed through
		{"/snap/bin/foo '-f %U'", []string{"/snap/bin/foo", "-f %U"}},
		{"/snap/bin/foo '-f %%bar'", []string{"/snap/bin/foo", "-f %%bar"}},
		{"/snap/bin/foo '-f %U %%bar'", []string{"/snap/bin/foo", "-f %U %%bar"}},
		{"/snap/bin/foo \"'-f bar'\"", []string{"/snap/bin/foo", "'-f bar'"}},
		{"/snap/bin/foo '\"-f bar\"'", []string{"/snap/bin/foo", "\"-f bar\""}},
		// valid with exec variables stripped out
		{"/snap/bin/foo -f %U", []string{"/snap/bin/foo", "-f"}},
		{"/snap/bin/foo -f %U %i", []string{"/snap/bin/foo", "-f", "--icon", "/snap/chromium/1193/chromium.png"}},
		{"/snap/bin/foo -f %U bar", []string{"/snap/bin/foo", "-f", "bar"}},
		{"/snap/bin/foo -f %U bar %i", []string{"/snap/bin/foo", "-f", "bar", "--icon", "/snap/chromium/1193/chromium.png"}},
		// valid with mixture of literal '%' and exec variables
		{"/snap/bin/foo -f %U %%bar", []string{"/snap/bin/foo", "-f", "%bar"}},
		{"/snap/bin/foo -f %U %i %%bar", []string{"/snap/bin/foo", "-f", "--icon", "/snap/chromium/1193/chromium.png", "%bar"}},
		{"/snap/bin/foo -f %U %%bar %i", []string{"/snap/bin/foo", "-f", "%bar", "--icon", "/snap/chromium/1193/chromium.png"}},
		{"/snap/bin/foo -f %%bar %U %i", []string{"/snap/bin/foo", "-f", "%bar", "--icon", "/snap/chromium/1193/chromium.png"}},
	}

	for _, test := range testCases {
		actual, err := userd.ParseExecCommand(test.cmd, "/snap/chromium/1193/chromium.png")
		comment := Commentf("cmd=%s", test.cmd)
		c.Check(err, IsNil, comment)
		c.Check(actual, DeepEquals, test.expect, comment)
	}
}

func (s *privilegedDesktopLauncherInternalSuite) TestParseExecCommandFailsWithInvalidEntry(c *C) {
	var testCases = []struct {
		cmd string
		err string
	}{
		// Commands may be rejected for bad quoting
		{`/snap/bin/foo "unclosed double quote`, "EOF found when expecting closing quote"},
		{`/snap/bin/foo 'unclosed single quote`, "EOF found when expecting closing quote"},

		// Or use of unexpected unknown variables
		{"/snap/bin/foo %z", `cannot run "/snap/bin/foo %z" due to use of "%z"`},
		{"/snap/bin/foo %", `cannot run "/snap/bin/foo %" due to use of "%"`},
	}

	for _, test := range testCases {
		_, err := userd.ParseExecCommand(test.cmd, "/snap/chromium/1193/chromium.png")
		comment := Commentf("cmd=%s", test.cmd)
		c.Check(err, ErrorMatches, test.err, comment)
	}
}

func (s *privilegedDesktopLauncherInternalSuite) TestReadExecCommandFromDesktopFileWithValidContent(c *C) {
	desktopFile := filepath.Join(c.MkDir(), "test.desktop")

	// We need to correct the embedded path to the desktop file before writing the file
	fileContent := strings.Replace(chromiumDesktopFile, "/var/lib/snapd/desktop/applications/chromium_chromium.desktop", desktopFile, -1)
	err := ioutil.WriteFile(desktopFile, []byte(fileContent), 0644)
	c.Assert(err, IsNil)

	exec, icon, err := userd.ReadExecCommandFromDesktopFile(desktopFile)
	c.Assert(err, IsNil)

	c.Check(exec, Equals, "env BAMF_DESKTOP_FILE_HINT="+desktopFile+" /snap/bin/chromium %U")
	c.Check(icon, Equals, "/snap/chromium/1193/chromium.png")
}

func (s *privilegedDesktopLauncherInternalSuite) TestReadExecCommandFromDesktopFileWithInvalidExec(c *C) {
	desktopFile := filepath.Join(c.MkDir(), "test.desktop")

	err := ioutil.WriteFile(desktopFile, []byte(chromiumDesktopFile), 0644)
	c.Assert(err, IsNil)

	_, _, err = userd.ReadExecCommandFromDesktopFile(desktopFile)
	c.Assert(err, ErrorMatches, `desktop file ".*" has an unsupported 'Exec' value: .*`)
}

func (s *privilegedDesktopLauncherInternalSuite) TestReadExecCommandFromDesktopFileWithNoDesktopEntry(c *C) {
	desktopFile := filepath.Join(c.MkDir(), "test.desktop")

	// We need to correct the embedded path to the desktop file before writing the file
	fileContent := strings.Replace(chromiumDesktopFile, "/var/lib/snapd/desktop/applications/chromium_chromium.desktop", desktopFile, -1)
	fileContent = strings.Replace(fileContent, "[Desktop Entry]", "[garbage]", -1)

	err := ioutil.WriteFile(desktopFile, []byte(fileContent), 0644)
	c.Assert(err, IsNil)

	_, _, err = userd.ReadExecCommandFromDesktopFile(desktopFile)
	c.Assert(err, ErrorMatches, `desktop file ".*" has an unsupported 'Exec' value: ""`)
}

func (s *privilegedDesktopLauncherInternalSuite) TestReadExecCOmmandFromDesktopFileMultipleDesktopEntrySections(c *C) {
	desktopFile := filepath.Join(c.MkDir(), "test.desktop")
	c.Assert(ioutil.WriteFile(desktopFile, []byte(`[Desktop Entry]
Exec=foo

[Desktop Entry]
Exec=bar
`), 0644), IsNil)

	_, _, err := userd.ReadExecCommandFromDesktopFile(desktopFile)
	c.Check(err, ErrorMatches, `desktop file ".*" has multiple \[Desktop Entry\] sections`)
}

func (s *privilegedDesktopLauncherInternalSuite) TestReadExecCommandFromDesktopFileWithNoFile(c *C) {
	desktopFile := filepath.Join(c.MkDir(), "test.desktop")

	_, _, err := userd.ReadExecCommandFromDesktopFile(desktopFile)
	c.Assert(err, ErrorMatches, `open .*: no such file or directory`)
}
