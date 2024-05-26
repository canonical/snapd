// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package desktopentry_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/desktop/desktopentry"
)

func Test(t *testing.T) { TestingT(t) }

type desktopentrySuite struct{}

var _ = Suite(&desktopentrySuite{})

const browserDesktopEntry = `
[Desktop Entry]
Version=1.0
Type=Application
Name=Web Browser
Exec=browser %u
Icon = ${SNAP}/default256.png
Actions=NewWindow;NewPrivateWindow;

[Something else]
Name=Not the app name
Exec=not-the-executable

# A comment
[Desktop Action NewWindow]
Name = Open a New Window
Exec=browser -new-window

[Desktop Action NewPrivateWindow]
Name=Open a New Private Window
Exec=browser -private-window
Icon=${SNAP}/private.png
`

func (s *desktopentrySuite) TestParse(c *C) {
	r := bytes.NewBufferString(browserDesktopEntry)
	de := mylog.Check2(desktopentry.Parse("/path/browser.desktop", r))


	c.Check(de.Name, Equals, "Web Browser")
	c.Check(de.Icon, Equals, "${SNAP}/default256.png")
	c.Check(de.Exec, Equals, "browser %u")
	c.Check(de.Actions, HasLen, 2)

	c.Assert(de.Actions["NewWindow"], NotNil)
	c.Check(de.Actions["NewWindow"].Name, Equals, "Open a New Window")
	c.Check(de.Actions["NewWindow"].Icon, Equals, "")
	c.Check(de.Actions["NewWindow"].Exec, Equals, "browser -new-window")

	c.Assert(de.Actions["NewPrivateWindow"], NotNil)
	c.Check(de.Actions["NewPrivateWindow"].Name, Equals, "Open a New Private Window")
	c.Check(de.Actions["NewPrivateWindow"].Icon, Equals, "${SNAP}/private.png")
	c.Check(de.Actions["NewPrivateWindow"].Exec, Equals, "browser -private-window")
}

func (s *desktopentrySuite) TestParseBad(c *C) {
	for i, tc := range []struct {
		in  string
		err string
	}{{
		in: `
[Desktop Entry]
[Desktop Entry]
`,
		err: `desktop file "/path/foo.desktop" has multiple \[Desktop Entry\] groups`,
	}, {
		in: `
[Desktop Entry]
Actions=known;
[Desktop Action known]
[Desktop Action unknown]
`,
		err: `desktop file "/path/foo.desktop" contains unknown action "unknown"`,
	}, {
		in: `
[Desktop Entry]
Actions=known;
[Desktop Action known]
[Desktop Action known]
`,
		err: `desktop file "/path/foo.desktop" has multiple "\[Desktop Action known\]" groups`,
	}, {
		in: `
[Desktop Entry]
NoEqualsSign
`,
		err: `desktop file "/path/foo.desktop" badly formed in line "NoEqualsSign"`,
	}} {
		c.Logf("tc %d", i)
		r := bytes.NewBufferString(tc.in)
		de := mylog.Check2(desktopentry.Parse("/path/foo.desktop", r))
		c.Check(de, IsNil)
		c.Check(err, ErrorMatches, tc.err)
	}
}

func (s *desktopentrySuite) TestRead(c *C) {
	path := filepath.Join(c.MkDir(), "foo.desktop")
	mylog.Check(os.WriteFile(path, []byte(browserDesktopEntry), 0o644))


	de := mylog.Check2(desktopentry.Read(path))

	c.Check(de.Filename, Equals, path)
	c.Check(de.Name, Equals, "Web Browser")
}

func (s *desktopentrySuite) TestReadNotFound(c *C) {
	path := filepath.Join(c.MkDir(), "foo.desktop")
	_ := mylog.Check2(desktopentry.Read(path))
	c.Check(err, ErrorMatches, `open .*: no such file or directory`)
}

func (s *desktopentrySuite) TestShouldAutostart(c *C) {
	allGood := `[Desktop Entry]
Exec=foo --bar
`
	hidden := `[Desktop Entry]
Exec=foo --bar
Hidden=true
`
	hiddenFalse := `[Desktop Entry]
Exec=foo --bar
Hidden=false
`
	justGNOME := `[Desktop Entry]
Exec=foo --bar
OnlyShowIn=GNOME;
`
	notInGNOME := `[Desktop Entry]
Exec=foo --bar
NotShownIn=GNOME;
`
	notInGNOMEAndKDE := `[Desktop Entry]
Exec=foo --bar
NotShownIn=GNOME;KDE;
`
	hiddenGNOMEextension := `[Desktop Entry]
Exec=foo --bar
X-GNOME-Autostart-enabled=false
`
	GNOMEextension := `[Desktop Entry]
Exec=foo --bar
X-GNOME-Autostart-enabled=true
`

	for i, tc := range []struct {
		in        string
		current   string
		autostart bool
	}{{
		in:        allGood,
		autostart: true,
	}, {
		in:        hidden,
		autostart: false,
	}, {
		in:        hiddenFalse,
		autostart: true,
	}, {
		in:        justGNOME,
		current:   "GNOME",
		autostart: true,
	}, {
		in:        justGNOME,
		current:   "ubuntu:GNOME",
		autostart: true,
	}, {
		in:        justGNOME,
		current:   "KDE",
		autostart: false,
	}, {
		in:        notInGNOME,
		current:   "GNOME",
		autostart: false,
	}, {
		in:        notInGNOME,
		current:   "ubuntu:GNOME",
		autostart: false,
	}, {
		in:        notInGNOME,
		current:   "KDE",
		autostart: true,
	}, {
		in:        notInGNOMEAndKDE,
		current:   "XFCE",
		autostart: true,
	}, {
		in:        notInGNOMEAndKDE,
		current:   "ubuntu:GNOME",
		autostart: false,
	}, {
		in:        hiddenGNOMEextension,
		current:   "GNOME",
		autostart: false,
	}, {
		in:        hiddenGNOMEextension,
		current:   "KDE",
		autostart: true,
	}, {
		in:        GNOMEextension,
		current:   "GNOME",
		autostart: true,
	}, {
		in:        GNOMEextension,
		current:   "KDE",
		autostart: true,
	}} {
		c.Logf("tc %d", i)
		r := bytes.NewBufferString(tc.in)
		de := mylog.Check2(desktopentry.Parse("/path/foo.desktop", r))
		c.Check(err, IsNil)
		currentDesktop := strings.Split(tc.current, ":")
		c.Check(de.ShouldAutostart(currentDesktop), Equals, tc.autostart)
	}
}

func (s *desktopentrySuite) TestExpandExec(c *C) {
	r := bytes.NewBufferString(browserDesktopEntry)
	de := mylog.Check2(desktopentry.Parse("/path/browser.desktop", r))


	args := mylog.Check2(de.ExpandExec([]string{"http://example.org"}))

	c.Check(args, DeepEquals, []string{"browser", "http://example.org"})

	// When called with no URIs, the %U code expands to nothing
	args = mylog.Check2(de.ExpandExec(nil))

	c.Check(args, DeepEquals, []string{"browser"})

	// If the Exec line is missing, an error is returned
	de.Exec = ""
	_ = mylog.Check2(de.ExpandExec(nil))
	c.Check(err, ErrorMatches, `desktop file "/path/browser.desktop" has no Exec line`)
}

func (s *desktopentrySuite) TestExpandActionExec(c *C) {
	r := bytes.NewBufferString(browserDesktopEntry)
	de := mylog.Check2(desktopentry.Parse("/path/browser.desktop", r))


	args := mylog.Check2(de.ExpandActionExec("NewWindow", nil))

	c.Check(args, DeepEquals, []string{"browser", "-new-window"})

	// Expanding a non-existent action, an error is returned
	_ = mylog.Check2(de.ExpandActionExec("UnknownAction", nil))
	c.Check(err, ErrorMatches, `desktop file "/path/browser.desktop" does not have action "UnknownAction"`)

	// If the action is missing its Exec line, an error is returned
	de.Actions["NewWindow"].Exec = ""
	_ = mylog.Check2(de.ExpandActionExec("NewWindow", nil))
	c.Check(err, ErrorMatches, `desktop file "/path/browser.desktop" action "NewWindow" has no Exec line`)
}

const chromiumDesktopEntry = `[Desktop Entry]
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

func (s *desktopentrySuite) TestParseChromiumDesktopEntry(c *C) {
	r := bytes.NewBufferString(chromiumDesktopEntry)
	de := mylog.Check2(desktopentry.Parse("/path/chromium_chromium.desktop", r))


	c.Check(de.Name, Equals, "Chromium Web Browser")
	c.Check(de.Icon, Equals, "/snap/chromium/1193/chromium.png")
	c.Check(de.Exec, Equals, "env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop /snap/bin/chromium %U")
	c.Check(de.Actions, HasLen, 3)

	c.Assert(de.Actions["NewWindow"], NotNil)
	c.Check(de.Actions["NewWindow"].Name, Equals, "Open a New Window")
	c.Check(de.Actions["NewWindow"].Icon, Equals, "")
	c.Check(de.Actions["NewWindow"].Exec, Equals, "env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop /snap/bin/chromium")

	c.Assert(de.Actions["Incognito"], NotNil)
	c.Check(de.Actions["Incognito"].Name, Equals, "Open a New Window in incognito mode")
	c.Check(de.Actions["Incognito"].Icon, Equals, "")
	c.Check(de.Actions["Incognito"].Exec, Equals, "env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop /snap/bin/chromium --incognito")

	c.Assert(de.Actions["TempProfile"], NotNil)
	c.Check(de.Actions["TempProfile"].Name, Equals, "Open a New Window with a temporary profile")
	c.Check(de.Actions["TempProfile"].Icon, Equals, "")
	c.Check(de.Actions["TempProfile"].Exec, Equals, "env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop /snap/bin/chromium --temp-profile")

	args := mylog.Check2(de.ExpandExec([]string{"http://example.org"}))

	c.Check(args, DeepEquals, []string{"env", "BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop", "/snap/bin/chromium", "http://example.org"})

	args = mylog.Check2(de.ExpandActionExec("Incognito", nil))

	c.Check(args, DeepEquals, []string{"env", "BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/chromium_chromium.desktop", "/snap/bin/chromium", "--incognito"})
}
