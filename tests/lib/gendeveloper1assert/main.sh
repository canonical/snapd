#!/bin/bash -e

if [ "$#" -eq 0 ] || [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    echo "usage: $0 <key-name> <system-user-assertion-json> <output-file>"
    exit 1
fi

ASSERTION_FILE="$1"
OUTPUT_FILE="${2:-auto-import.assert}"

if ! command -v gendeveloper1model; then
    echo "Command gendeveloper1model not found, please build the tool doing:"
    echo "$ go install ./tests/lib/gendeveloper1model"
    exit 1
fi

# sign the assertion
sysUser="$(gendeveloper1model < "$ASSERTION_FILE")"

cat >> "$OUTPUT_FILE" << EOF
type: account
authority-id: testrootorg
account-id: developer1
display-name: Developer1
timestamp: 2020-09-11T11:43:46-05:00
username: developer1
validation: unproven
sign-key-sha3-384: hIedp1AvrWlcDI4uS_qjoFLzjKl5enu4G2FYJpgB3Pj-tUzGlTQBxMBsBmi-tnJR

AcLBUgQAAQoABgUCX1upQgAAa6sQALM/XTz+4h0t9G/eHTrPYCVCztPugC9ZyTLtqRgdvv2JIl4p
oBK7/bByBE8K6Qhp8koAcXQ/PVzjMFrW1bs2g6BODPvoy9g0DKMgDZRxMXT7F9Uv006qBWED0D2g
utsUeYYpCBDOAV843rvwaXYDtTvNQngTaQQ2EGO6XODtGUkRVyjFS3KG+EfbRVtf9gx6VvkrbBLR
FSAtd22uKmfD35FUZuUHFszZ/mRZ0OwF40V74vl1EXTWnRxuqzSH47FiWfOYYYcODPDLzPRxyP6T
vKqoRQ4vPr+GhrmtLoTInuC9KAoLF2CHto9NUAoPX79/RF967Utv9URpmKZsBQItdWnx83bmLVYc
soA3wLS74W/VHfYNFJi1F6Nw2yKewVfyVfq/Cd2IcFO0B/+Rk05+1H3d0JWKj4nDSSVgIN7DdhBQ
5iIxhEZb5G9taLMFE8xfMS1+Ss5MMoMs/VPoojlJhJimNmgcatj58ygQNaX07pJ+mcMYT6XAQ9gx
9SwNOoDXqf6lf6khtJk1zBQwLCc0Yslb7f/CwCDBVht1ycwyZvnLMDw2Ohlj/tQ9KSJAVflqCFDm
ccH4uhHFMgD8SNRASS10LU2GIH+8Rqw5wkj6VXEkoMIHERAqnwR7/9ByCHLiDs1QUTL7ncRWuEq5
eYCJRESnQP9guEFOoUmOO+gkDpCo

type: account-key
authority-id: testrootorg
public-key-sha3-384: EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu
account-id: developer1
name: default
since: 2020-09-11T11:47:45-05:00
body-length: 717
sign-key-sha3-384: hIedp1AvrWlcDI4uS_qjoFLzjKl5enu4G2FYJpgB3Pj-tUzGlTQBxMBsBmi-tnJR

AcbBTQRWhcGAARAAtJGIguK7FhSyRxL/6jvdy0zAgGCjC1xVNFzeF76p5G8BXNEEHZUHK+z8Gr2J
inVrpvhJhllf5Ob2dIMH2YQbC9jE1kjbzvuauQGDqk6tNQm0i3KDeHCSPgVN+PFXPwKIiLrh66Po
AC7OfR1rFUgCqu0jch0H6Nue0ynvEPiY4dPeXq7mCdpDr5QIAM41L+3hg0OdzvO8HMIGZQpdF6jP
7fkkVMROYvHUOJ8kknpKE7FiaNNpH7jK1qNxOYhLeiioX0LYrdmTvdTWHrSKZc82ZmlDjpKc4hUx
VtTXMAysw7CzIdREPom/vJklnKLvZt+Wk5AEF5V5YKnuT3pY+fjVMZ56GtTEeO/Er/oLk/n2xUK5
fD5DAyW/9z0ygzwTbY5IuWXyDfYneL4nXwWOEgg37Z4+8mTH+ftTz2dl1x1KIlIR2xo0kxf9t8K+
jlr13vwF1+QReMCSUycUsZ2Eep5XhjI+LG7G1bMSGqodZTIOXLkIy6+3iJ8Z/feIHlJ0ELBDyFbl
Yy04Sf9LI148vJMsYenonkoWejWdMi8iCUTeaZydHJEUBU/RbNFLjCWa6NIUe9bfZgLiOOZkps54
+/AL078ri/tGjo/5UGvezSmwrEoWJyqrJt2M69N2oVDLJcHeo2bUYPtFC2Kfb2je58JrJ+llifdg
rAsxbnHXiXyVimUAEQEAAQ==

AcLBUgQAAQoABgUCX1uqMQAAZZ0QACLpYbT+5zRcluR6I4IPyGrVgTM19L7x4rxlOIU5wdRdM6xi
Dnl49fj7XaC8hNNsZ+lKe8VvjZU44gMFtvooBY6nsub5wiaG3PwR4Oed9J4p6Xv5DrrFTLUy62sV
/ApfcQwcTgZPt9PHAvr4nWKrl0ierxfIdgdPBV5w+mEOPHSCx/JyX8b6BhDHgH07NEGvFWj0by9I
g1VCmxNceH+a0loYizU1S3LJnBGD89W0UMq1HRO8stBIH5VC4VeaOppnj8MnnVUriF90lK26YzGR
fFZsg/f5S4CC0lHCh+6jvIAjOFinmCg8ouI5ja2c54nWQ/D1ZHjNqvEC8fVNWf3VYYZUIrKDMdCz
WNvtfk44bBp9mHKIPiS3Sn5xNK8pwZ/ldmAiLMZSovIYzocHqeIUAos7M8lth1lLrGo29Iwv6lof
tfpJT8QKkrKU63tCOuvw6W1pmfU/LU91cEuq4dVE+77GpDiOo66LsSHZzLMQb+fkjOnK79swJ6Bw
AqHJYdx/mNU1/N9FeUbzgESjSY6OydjWhAB6F/T5S8O/K8SExF2+wQIOw+A+D80QbrpbcsL5Yzcb
gqS5rFAxK1vPvMUk/t7pXpcoR9t+KGqsNhMiG0AsFqckZi0A2F5XzUztF2MtDLUdNaxmEC7ZITB/
tgWXS3ChGQqx9v3dl0VqKP/Pkad/

$sysUser
EOF
