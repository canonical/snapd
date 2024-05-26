// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2020 Canonical Ltd
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

package main

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snapdenv"
)

const (
	encodedRepairRootAccountKey = `type: account-key
authority-id: canonical
public-key-sha3-384: nttW6NfBXI_E-00u38W-KH6eiksfQNXuI7IiumoV49_zkbhM0sYTzSnFlwZC-W4t
account-id: canonical
name: repair-root
since: 2017-07-07T00:00:00.0Z
body-length: 1406
sign-key-sha3-384: -CvQKAwRQ5h3Ffn10FILJoEZUXOv6km9FwA80-Rcj-f-6jadQ89VRswHNiEB9Lxk

AcbDTQRWhcGAASAAtlCIEilQCQh9Ffjn9IxD+5FtWfdKdJHrQdtFPy/2Q1kOvC++ef/3bG1xMwao
tue9K0HCMtv3apHS1C32Y8JBMD8oykRpAd5H05+sgzZr3kCHIvgogKFsXfdd5+W5Q1+59Vy/81UH
tJCs99wBwboNh/pMCXBGI3jDRN1f7hOxcHUIW+KTaHCVZnrXXmCn6Oe6brR9qiUXgEB2I6rBT/Fe
cumdfvFN/zSsJ3Vvv9IbTfHYAZD82NrSqz4UZ3WJarIaxlykgLJaZN4bqSQYPYsc8lLlwjQeGloW
+r8dIypKOzPnUYurzWcNzcCnNCT1zhpY/IK2rFcZbN5/mP2/5PtjFlbX88aPGPbOTqYANmxfboCx
wo4D4aS7PD6gLC7XM8bgh8BACpmG3BnskL7F/9IHMl85SUHFIya2fDu7A7HqNUn7cpENGbHojj7G
J2s2965FSRuIvp69wEmknYD/kahjT1+Vy94D2rVB7mjtTruPueF2KTpo2jRXFM+ABq+T9ybjXD6f
UuSXu5xeg0Cv1sxOh4O4b45uaCXb8B74chEUW+cb3cV0NGE/QgBJUBeS68vUI8lqQFmPInci6Md4
oiKFVbloL0ZmOGj73Xv2uAcexAK9bEiI+adVS2x9r4eFwtkST3XG0t/kw7eLgAVjtRcpmD6EuZ0Q
ulAJHEsl7Sazm8GRU4GtZWaCajVb4n5TS1lin2nqUXwqxRUA3smLqkQoYXQni9vmhez4dEP4MVvq
/0IdI50UGME5Fzc8hUhYzvbNS8g+VOeAK/qj3dzcdF9+n940AQI16Fcxi1/xFk8A4dw3AaDl4XnJ
piyStE+xi95ne5HJW8r/f/JQos8I6QR5w7pe2URbgUdVPgQLv3r/4dS/X3aP+oakrPR7JuAVdP62
vsjF4jK8Bl69mcF434xpshnbnW/f7XHomPY4gp8y7kD2/DdEs5hvaTHIPp25DEYhqjt3gfmMuUXi
Mb5oy9KZp3ff8Squ+XNWSGQSyhX14xcQwM8QjNQnAisNg2hYwSM2n8q5IDWiwJQkFSriP5tMsa8E
DMGI3LXUZKRJll9dQBjs6VzApT4/Ee0ZvSni0d2cWm3wkqFQudRpqz3JSwQ7jal0W5e0UhNeHh/W
7nACD5hvcwF7UgUz0r8adlOy+nyfvWte65nbcRrIH7GS1xdgS0e9eW4znsplp7s/Z3gMhi8CN5TY
0nZW82TTl69Wvn13SGJye0yJSjiy4KS0iRE6BwAt7dGAMs5c62IlBsWEHLmCW1/lixWA9YXT9iji
G7DKSoofnsvqVP2wIQZxxt4xHMjUGXecyx8QX4BznwsV1vbzHOIG4a3Z9A1X1L3yh3ZbazFVeEE9
7Dhz9hGYfd3PvwARAQAB

AcLDXAQAAQoABgUCWbuO2gAKCRDUpVvql9g3IOPcIADZWObdYMKh2SblWXxUchnc3S4LDRtL8v+Q
HdnXO5+dJmsj5LWhdsB7lz373aaylFTwHpNDWcDdAu7ulP0vr7zJURo1jGOo7VojSEeuAAu3YhwL
2pR0p5Me0wuxl/pCX0x0nfDSeeTw11kproyN0GwJaErKEmyQyfOgVr2jN5sl1gBqQtKgG5gqZzC3
oFH1HYGPl2kfAorxFw7MoPy4aRFaxUJfx4x6bEktgkkFT7AWGmawVwcpiiUbbpe9CPLEsn6yqJI9
5XmQ3dJjp/6Y5D7x04LRH3Q5fajRcpdBrC0tDsT9UDbSRtIyo0KDNVHwQalLa2Sv51DV+Fy4eneM
Lgu+oCUOnBecXIWnX+k0uyDW8aLHYapx8etpW3pln/hMRd8JxYVYAqDn7G2AYeSGS/4lzCJzysW2
2/4RhH9Ql8ea0nSWVTJr3pmXKlPSH/OOy9IADEDUuEdvyMcq3YOXA9E4L3g9vR31JH+++swcTQPz
rnGx0mE+TCQRWok/NZ1QNv4eNZlnLXdNS1DoV/kRqU04cblYYUoSO34mkjPEJ8ti+VzKh/PTA6/7
1feFX276Zam/6b2rBLWCWYdblDM9oLAR4PfzntBZW4LzzOIb95IwiK4JoDoBr3x4+RxvxgVQLvHt
8990uQ0se9+8BtLVFtd07NbldHXRBlZkq22a8CeVYrU3YZEtuEBvlGDpkdegw/vcvgHUUK1f8dXJ
0+9oW2yQOLAguuPZ67GFSgdTnvR5dQYZZR2EbQJlPMOFA3loKeUxHSR9w7E3SFqXGqN1v6APDK0V
lpVFq+rYlprvbf4GB0oR8yQOGtlxf+Ag3Nnx+90nlmtTK4k7tQpLzuAXGGREDCtn0y/YvWvGt6kN
EV5Q/mAVe2/CtAUvfyX7V3xlzYCrJT9DBcCBMaUUekFrwvZi13WYJIn7YE2Qmam7ZsXdb991PoFv
+c6Pmeg6w3y7D+Vj4Yfi8IrjPrc6765DaaZxFyMia9GEQKHChZDkiEiAM6RfwlC5YXGzCroaZi0Y
Knf/UkUWoa/jKZgQNiqrZ9oGmbURLeXkkHzpcFitwjzWr6tNScCzNIqs/uxTxbFM8fJu1gSmauEY
TE1rn62SiuHNRKJqfLcCHucStK10knHkHTAJ3avS7rBz0Dy8UOa77bOjyei5n2rkyXztL2YjjGYh
8jEt00xcvwJGePBfH10gCgTFWdfhfcP9/muKgiOSErQlHPypnr4vqO0PU9XDp106FFWyyNPd95kC
l5IF9WMfl7YHpT0Ph7kBYwg9sKF/7oCVdbT5CoImxkE5DTkWB8xX6W/BhuMrp1rzTHFFGVd1ppb7
EMUll4dd78OWonMlIgsMRuTSn93awb4X8xSJhRi9
`
)

func init() {
	repairRootAccountKey := mylog.Check2(asserts.Decode([]byte(encodedRepairRootAccountKey)))

	if !snapdenv.UseStagingStore() {
		trustedRepairRootKeys = append(trustedRepairRootKeys, repairRootAccountKey.(*asserts.AccountKey))
	}
}
