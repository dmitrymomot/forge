package country

// ISO-3166-1 static data: alpha-2/alpha-3/numeric codes, English short name,
// primary official ISO-4217 currency, and E.164 dial code. Curated static data
// committed to the repo — no runtime fetch. Emoji flags are derived from the
// alpha-2 pair at package init. Source: ISO-3166-1 (2026 edition).

var (
	AD = Country{Alpha2: "AD", Alpha3: "AND", Numeric: "020", Name: "Andorra", Currency: "EUR", DialCode: "376"}
	AE = Country{Alpha2: "AE", Alpha3: "ARE", Numeric: "784", Name: "United Arab Emirates", Currency: "AED", DialCode: "971"}
	AF = Country{Alpha2: "AF", Alpha3: "AFG", Numeric: "004", Name: "Afghanistan", Currency: "AFN", DialCode: "93"}
	AG = Country{Alpha2: "AG", Alpha3: "ATG", Numeric: "028", Name: "Antigua and Barbuda", Currency: "XCD", DialCode: "1"}
	AI = Country{Alpha2: "AI", Alpha3: "AIA", Numeric: "660", Name: "Anguilla", Currency: "XCD", DialCode: "1"}
	AL = Country{Alpha2: "AL", Alpha3: "ALB", Numeric: "008", Name: "Albania", Currency: "ALL", DialCode: "355"}
	AM = Country{Alpha2: "AM", Alpha3: "ARM", Numeric: "051", Name: "Armenia", Currency: "AMD", DialCode: "374"}
	AO = Country{Alpha2: "AO", Alpha3: "AGO", Numeric: "024", Name: "Angola", Currency: "AOA", DialCode: "244"}
	AQ = Country{Alpha2: "AQ", Alpha3: "ATA", Numeric: "010", Name: "Antarctica", Currency: "USD", DialCode: "672"}
	AR = Country{Alpha2: "AR", Alpha3: "ARG", Numeric: "032", Name: "Argentina", Currency: "ARS", DialCode: "54"}
	AS = Country{Alpha2: "AS", Alpha3: "ASM", Numeric: "016", Name: "American Samoa", Currency: "USD", DialCode: "1"}
	AT = Country{Alpha2: "AT", Alpha3: "AUT", Numeric: "040", Name: "Austria", Currency: "EUR", DialCode: "43"}
	AU = Country{Alpha2: "AU", Alpha3: "AUS", Numeric: "036", Name: "Australia", Currency: "AUD", DialCode: "61"}
	AW = Country{Alpha2: "AW", Alpha3: "ABW", Numeric: "533", Name: "Aruba", Currency: "AWG", DialCode: "297"}
	AX = Country{Alpha2: "AX", Alpha3: "ALA", Numeric: "248", Name: "Åland Islands", Currency: "EUR", DialCode: "358"}
	AZ = Country{Alpha2: "AZ", Alpha3: "AZE", Numeric: "031", Name: "Azerbaijan", Currency: "AZN", DialCode: "994"}
	BA = Country{Alpha2: "BA", Alpha3: "BIH", Numeric: "070", Name: "Bosnia and Herzegovina", Currency: "BAM", DialCode: "387"}
	BB = Country{Alpha2: "BB", Alpha3: "BRB", Numeric: "052", Name: "Barbados", Currency: "BBD", DialCode: "1"}
	BD = Country{Alpha2: "BD", Alpha3: "BGD", Numeric: "050", Name: "Bangladesh", Currency: "BDT", DialCode: "880"}
	BE = Country{Alpha2: "BE", Alpha3: "BEL", Numeric: "056", Name: "Belgium", Currency: "EUR", DialCode: "32"}
	BF = Country{Alpha2: "BF", Alpha3: "BFA", Numeric: "854", Name: "Burkina Faso", Currency: "XOF", DialCode: "226"}
	BG = Country{Alpha2: "BG", Alpha3: "BGR", Numeric: "100", Name: "Bulgaria", Currency: "BGN", DialCode: "359"}
	BH = Country{Alpha2: "BH", Alpha3: "BHR", Numeric: "048", Name: "Bahrain", Currency: "BHD", DialCode: "973"}
	BI = Country{Alpha2: "BI", Alpha3: "BDI", Numeric: "108", Name: "Burundi", Currency: "BIF", DialCode: "257"}
	BJ = Country{Alpha2: "BJ", Alpha3: "BEN", Numeric: "204", Name: "Benin", Currency: "XOF", DialCode: "229"}
	BL = Country{Alpha2: "BL", Alpha3: "BLM", Numeric: "652", Name: "Saint Barthélemy", Currency: "EUR", DialCode: "590"}
	BM = Country{Alpha2: "BM", Alpha3: "BMU", Numeric: "060", Name: "Bermuda", Currency: "BMD", DialCode: "1"}
	BN = Country{Alpha2: "BN", Alpha3: "BRN", Numeric: "096", Name: "Brunei Darussalam", Currency: "BND", DialCode: "673"}
	BO = Country{Alpha2: "BO", Alpha3: "BOL", Numeric: "068", Name: "Bolivia", Currency: "BOB", DialCode: "591"}
	BQ = Country{Alpha2: "BQ", Alpha3: "BES", Numeric: "535", Name: "Bonaire, Sint Eustatius and Saba", Currency: "USD", DialCode: "599"}
	BR = Country{Alpha2: "BR", Alpha3: "BRA", Numeric: "076", Name: "Brazil", Currency: "BRL", DialCode: "55"}
	BS = Country{Alpha2: "BS", Alpha3: "BHS", Numeric: "044", Name: "Bahamas", Currency: "BSD", DialCode: "1"}
	BT = Country{Alpha2: "BT", Alpha3: "BTN", Numeric: "064", Name: "Bhutan", Currency: "BTN", DialCode: "975"}
	BV = Country{Alpha2: "BV", Alpha3: "BVT", Numeric: "074", Name: "Bouvet Island", Currency: "NOK", DialCode: "47"}
	BW = Country{Alpha2: "BW", Alpha3: "BWA", Numeric: "072", Name: "Botswana", Currency: "BWP", DialCode: "267"}
	BY = Country{Alpha2: "BY", Alpha3: "BLR", Numeric: "112", Name: "Belarus", Currency: "BYN", DialCode: "375"}
	BZ = Country{Alpha2: "BZ", Alpha3: "BLZ", Numeric: "084", Name: "Belize", Currency: "BZD", DialCode: "501"}
	CA = Country{Alpha2: "CA", Alpha3: "CAN", Numeric: "124", Name: "Canada", Currency: "CAD", DialCode: "1"}
	CC = Country{Alpha2: "CC", Alpha3: "CCK", Numeric: "166", Name: "Cocos (Keeling) Islands", Currency: "AUD", DialCode: "61"}
	CD = Country{Alpha2: "CD", Alpha3: "COD", Numeric: "180", Name: "Congo (Democratic Republic)", Currency: "CDF", DialCode: "243"}
	CF = Country{Alpha2: "CF", Alpha3: "CAF", Numeric: "140", Name: "Central African Republic", Currency: "XAF", DialCode: "236"}
	CG = Country{Alpha2: "CG", Alpha3: "COG", Numeric: "178", Name: "Congo", Currency: "XAF", DialCode: "242"}
	CH = Country{Alpha2: "CH", Alpha3: "CHE", Numeric: "756", Name: "Switzerland", Currency: "CHF", DialCode: "41"}
	CI = Country{Alpha2: "CI", Alpha3: "CIV", Numeric: "384", Name: "Côte d'Ivoire", Currency: "XOF", DialCode: "225"}
	CK = Country{Alpha2: "CK", Alpha3: "COK", Numeric: "184", Name: "Cook Islands", Currency: "NZD", DialCode: "682"}
	CL = Country{Alpha2: "CL", Alpha3: "CHL", Numeric: "152", Name: "Chile", Currency: "CLP", DialCode: "56"}
	CM = Country{Alpha2: "CM", Alpha3: "CMR", Numeric: "120", Name: "Cameroon", Currency: "XAF", DialCode: "237"}
	CN = Country{Alpha2: "CN", Alpha3: "CHN", Numeric: "156", Name: "China", Currency: "CNY", DialCode: "86"}
	CO = Country{Alpha2: "CO", Alpha3: "COL", Numeric: "170", Name: "Colombia", Currency: "COP", DialCode: "57"}
	CR = Country{Alpha2: "CR", Alpha3: "CRI", Numeric: "188", Name: "Costa Rica", Currency: "CRC", DialCode: "506"}
	CU = Country{Alpha2: "CU", Alpha3: "CUB", Numeric: "192", Name: "Cuba", Currency: "CUP", DialCode: "53"}
	CV = Country{Alpha2: "CV", Alpha3: "CPV", Numeric: "132", Name: "Cabo Verde", Currency: "CVE", DialCode: "238"}
	CW = Country{Alpha2: "CW", Alpha3: "CUW", Numeric: "531", Name: "Curaçao", Currency: "ANG", DialCode: "599"}
	CX = Country{Alpha2: "CX", Alpha3: "CXR", Numeric: "162", Name: "Christmas Island", Currency: "AUD", DialCode: "61"}
	CY = Country{Alpha2: "CY", Alpha3: "CYP", Numeric: "196", Name: "Cyprus", Currency: "EUR", DialCode: "357"}
	CZ = Country{Alpha2: "CZ", Alpha3: "CZE", Numeric: "203", Name: "Czechia", Currency: "CZK", DialCode: "420"}
	DE = Country{Alpha2: "DE", Alpha3: "DEU", Numeric: "276", Name: "Germany", Currency: "EUR", DialCode: "49"}
	DJ = Country{Alpha2: "DJ", Alpha3: "DJI", Numeric: "262", Name: "Djibouti", Currency: "DJF", DialCode: "253"}
	DK = Country{Alpha2: "DK", Alpha3: "DNK", Numeric: "208", Name: "Denmark", Currency: "DKK", DialCode: "45"}
	DM = Country{Alpha2: "DM", Alpha3: "DMA", Numeric: "212", Name: "Dominica", Currency: "XCD", DialCode: "1"}
	DO = Country{Alpha2: "DO", Alpha3: "DOM", Numeric: "214", Name: "Dominican Republic", Currency: "DOP", DialCode: "1"}
	DZ = Country{Alpha2: "DZ", Alpha3: "DZA", Numeric: "012", Name: "Algeria", Currency: "DZD", DialCode: "213"}
	EC = Country{Alpha2: "EC", Alpha3: "ECU", Numeric: "218", Name: "Ecuador", Currency: "USD", DialCode: "593"}
	EE = Country{Alpha2: "EE", Alpha3: "EST", Numeric: "233", Name: "Estonia", Currency: "EUR", DialCode: "372"}
	EG = Country{Alpha2: "EG", Alpha3: "EGY", Numeric: "818", Name: "Egypt", Currency: "EGP", DialCode: "20"}
	EH = Country{Alpha2: "EH", Alpha3: "ESH", Numeric: "732", Name: "Western Sahara", Currency: "MAD", DialCode: "212"}
	ER = Country{Alpha2: "ER", Alpha3: "ERI", Numeric: "232", Name: "Eritrea", Currency: "ERN", DialCode: "291"}
	ES = Country{Alpha2: "ES", Alpha3: "ESP", Numeric: "724", Name: "Spain", Currency: "EUR", DialCode: "34"}
	ET = Country{Alpha2: "ET", Alpha3: "ETH", Numeric: "231", Name: "Ethiopia", Currency: "ETB", DialCode: "251"}
	FI = Country{Alpha2: "FI", Alpha3: "FIN", Numeric: "246", Name: "Finland", Currency: "EUR", DialCode: "358"}
	FJ = Country{Alpha2: "FJ", Alpha3: "FJI", Numeric: "242", Name: "Fiji", Currency: "FJD", DialCode: "679"}
	FK = Country{Alpha2: "FK", Alpha3: "FLK", Numeric: "238", Name: "Falkland Islands", Currency: "FKP", DialCode: "500"}
	FM = Country{Alpha2: "FM", Alpha3: "FSM", Numeric: "583", Name: "Micronesia", Currency: "USD", DialCode: "691"}
	FO = Country{Alpha2: "FO", Alpha3: "FRO", Numeric: "234", Name: "Faroe Islands", Currency: "DKK", DialCode: "298"}
	FR = Country{Alpha2: "FR", Alpha3: "FRA", Numeric: "250", Name: "France", Currency: "EUR", DialCode: "33"}
	GA = Country{Alpha2: "GA", Alpha3: "GAB", Numeric: "266", Name: "Gabon", Currency: "XAF", DialCode: "241"}
	GB = Country{Alpha2: "GB", Alpha3: "GBR", Numeric: "826", Name: "United Kingdom", Currency: "GBP", DialCode: "44"}
	GD = Country{Alpha2: "GD", Alpha3: "GRD", Numeric: "308", Name: "Grenada", Currency: "XCD", DialCode: "1"}
	GE = Country{Alpha2: "GE", Alpha3: "GEO", Numeric: "268", Name: "Georgia", Currency: "GEL", DialCode: "995"}
	GF = Country{Alpha2: "GF", Alpha3: "GUF", Numeric: "254", Name: "French Guiana", Currency: "EUR", DialCode: "594"}
	GG = Country{Alpha2: "GG", Alpha3: "GGY", Numeric: "831", Name: "Guernsey", Currency: "GBP", DialCode: "44"}
	GH = Country{Alpha2: "GH", Alpha3: "GHA", Numeric: "288", Name: "Ghana", Currency: "GHS", DialCode: "233"}
	GI = Country{Alpha2: "GI", Alpha3: "GIB", Numeric: "292", Name: "Gibraltar", Currency: "GIP", DialCode: "350"}
	GL = Country{Alpha2: "GL", Alpha3: "GRL", Numeric: "304", Name: "Greenland", Currency: "DKK", DialCode: "299"}
	GM = Country{Alpha2: "GM", Alpha3: "GMB", Numeric: "270", Name: "Gambia", Currency: "GMD", DialCode: "220"}
	GN = Country{Alpha2: "GN", Alpha3: "GIN", Numeric: "324", Name: "Guinea", Currency: "GNF", DialCode: "224"}
	GP = Country{Alpha2: "GP", Alpha3: "GLP", Numeric: "312", Name: "Guadeloupe", Currency: "EUR", DialCode: "590"}
	GQ = Country{Alpha2: "GQ", Alpha3: "GNQ", Numeric: "226", Name: "Equatorial Guinea", Currency: "XAF", DialCode: "240"}
	GR = Country{Alpha2: "GR", Alpha3: "GRC", Numeric: "300", Name: "Greece", Currency: "EUR", DialCode: "30"}
	GS = Country{Alpha2: "GS", Alpha3: "SGS", Numeric: "239", Name: "South Georgia and the South Sandwich Islands", Currency: "GBP", DialCode: "500"}
	GT = Country{Alpha2: "GT", Alpha3: "GTM", Numeric: "320", Name: "Guatemala", Currency: "GTQ", DialCode: "502"}
	GU = Country{Alpha2: "GU", Alpha3: "GUM", Numeric: "316", Name: "Guam", Currency: "USD", DialCode: "1"}
	GW = Country{Alpha2: "GW", Alpha3: "GNB", Numeric: "624", Name: "Guinea-Bissau", Currency: "XOF", DialCode: "245"}
	GY = Country{Alpha2: "GY", Alpha3: "GUY", Numeric: "328", Name: "Guyana", Currency: "GYD", DialCode: "592"}
	HK = Country{Alpha2: "HK", Alpha3: "HKG", Numeric: "344", Name: "Hong Kong", Currency: "HKD", DialCode: "852"}
	HM = Country{Alpha2: "HM", Alpha3: "HMD", Numeric: "334", Name: "Heard Island and McDonald Islands", Currency: "AUD", DialCode: "672"}
	HN = Country{Alpha2: "HN", Alpha3: "HND", Numeric: "340", Name: "Honduras", Currency: "HNL", DialCode: "504"}
	HR = Country{Alpha2: "HR", Alpha3: "HRV", Numeric: "191", Name: "Croatia", Currency: "EUR", DialCode: "385"}
	HT = Country{Alpha2: "HT", Alpha3: "HTI", Numeric: "332", Name: "Haiti", Currency: "HTG", DialCode: "509"}
	HU = Country{Alpha2: "HU", Alpha3: "HUN", Numeric: "348", Name: "Hungary", Currency: "HUF", DialCode: "36"}
	ID = Country{Alpha2: "ID", Alpha3: "IDN", Numeric: "360", Name: "Indonesia", Currency: "IDR", DialCode: "62"}
	IE = Country{Alpha2: "IE", Alpha3: "IRL", Numeric: "372", Name: "Ireland", Currency: "EUR", DialCode: "353"}
	IL = Country{Alpha2: "IL", Alpha3: "ISR", Numeric: "376", Name: "Israel", Currency: "ILS", DialCode: "972"}
	IM = Country{Alpha2: "IM", Alpha3: "IMN", Numeric: "833", Name: "Isle of Man", Currency: "GBP", DialCode: "44"}
	IN = Country{Alpha2: "IN", Alpha3: "IND", Numeric: "356", Name: "India", Currency: "INR", DialCode: "91"}
	IO = Country{Alpha2: "IO", Alpha3: "IOT", Numeric: "086", Name: "British Indian Ocean Territory", Currency: "USD", DialCode: "246"}
	IQ = Country{Alpha2: "IQ", Alpha3: "IRQ", Numeric: "368", Name: "Iraq", Currency: "IQD", DialCode: "964"}
	IR = Country{Alpha2: "IR", Alpha3: "IRN", Numeric: "364", Name: "Iran", Currency: "IRR", DialCode: "98"}
	IS = Country{Alpha2: "IS", Alpha3: "ISL", Numeric: "352", Name: "Iceland", Currency: "ISK", DialCode: "354"}
	IT = Country{Alpha2: "IT", Alpha3: "ITA", Numeric: "380", Name: "Italy", Currency: "EUR", DialCode: "39"}
	JE = Country{Alpha2: "JE", Alpha3: "JEY", Numeric: "832", Name: "Jersey", Currency: "GBP", DialCode: "44"}
	JM = Country{Alpha2: "JM", Alpha3: "JAM", Numeric: "388", Name: "Jamaica", Currency: "JMD", DialCode: "1"}
	JO = Country{Alpha2: "JO", Alpha3: "JOR", Numeric: "400", Name: "Jordan", Currency: "JOD", DialCode: "962"}
	JP = Country{Alpha2: "JP", Alpha3: "JPN", Numeric: "392", Name: "Japan", Currency: "JPY", DialCode: "81"}
	KE = Country{Alpha2: "KE", Alpha3: "KEN", Numeric: "404", Name: "Kenya", Currency: "KES", DialCode: "254"}
	KG = Country{Alpha2: "KG", Alpha3: "KGZ", Numeric: "417", Name: "Kyrgyzstan", Currency: "KGS", DialCode: "996"}
	KH = Country{Alpha2: "KH", Alpha3: "KHM", Numeric: "116", Name: "Cambodia", Currency: "KHR", DialCode: "855"}
	KI = Country{Alpha2: "KI", Alpha3: "KIR", Numeric: "296", Name: "Kiribati", Currency: "AUD", DialCode: "686"}
	KM = Country{Alpha2: "KM", Alpha3: "COM", Numeric: "174", Name: "Comoros", Currency: "KMF", DialCode: "269"}
	KN = Country{Alpha2: "KN", Alpha3: "KNA", Numeric: "659", Name: "Saint Kitts and Nevis", Currency: "XCD", DialCode: "1"}
	KP = Country{Alpha2: "KP", Alpha3: "PRK", Numeric: "408", Name: "North Korea", Currency: "KPW", DialCode: "850"}
	KR = Country{Alpha2: "KR", Alpha3: "KOR", Numeric: "410", Name: "South Korea", Currency: "KRW", DialCode: "82"}
	KW = Country{Alpha2: "KW", Alpha3: "KWT", Numeric: "414", Name: "Kuwait", Currency: "KWD", DialCode: "965"}
	KY = Country{Alpha2: "KY", Alpha3: "CYM", Numeric: "136", Name: "Cayman Islands", Currency: "KYD", DialCode: "1"}
	KZ = Country{Alpha2: "KZ", Alpha3: "KAZ", Numeric: "398", Name: "Kazakhstan", Currency: "KZT", DialCode: "7"}
	LA = Country{Alpha2: "LA", Alpha3: "LAO", Numeric: "418", Name: "Laos", Currency: "LAK", DialCode: "856"}
	LB = Country{Alpha2: "LB", Alpha3: "LBN", Numeric: "422", Name: "Lebanon", Currency: "LBP", DialCode: "961"}
	LC = Country{Alpha2: "LC", Alpha3: "LCA", Numeric: "662", Name: "Saint Lucia", Currency: "XCD", DialCode: "1"}
	LI = Country{Alpha2: "LI", Alpha3: "LIE", Numeric: "438", Name: "Liechtenstein", Currency: "CHF", DialCode: "423"}
	LK = Country{Alpha2: "LK", Alpha3: "LKA", Numeric: "144", Name: "Sri Lanka", Currency: "LKR", DialCode: "94"}
	LR = Country{Alpha2: "LR", Alpha3: "LBR", Numeric: "430", Name: "Liberia", Currency: "LRD", DialCode: "231"}
	LS = Country{Alpha2: "LS", Alpha3: "LSO", Numeric: "426", Name: "Lesotho", Currency: "LSL", DialCode: "266"}
	LT = Country{Alpha2: "LT", Alpha3: "LTU", Numeric: "440", Name: "Lithuania", Currency: "EUR", DialCode: "370"}
	LU = Country{Alpha2: "LU", Alpha3: "LUX", Numeric: "442", Name: "Luxembourg", Currency: "EUR", DialCode: "352"}
	LV = Country{Alpha2: "LV", Alpha3: "LVA", Numeric: "428", Name: "Latvia", Currency: "EUR", DialCode: "371"}
	LY = Country{Alpha2: "LY", Alpha3: "LBY", Numeric: "434", Name: "Libya", Currency: "LYD", DialCode: "218"}
	MA = Country{Alpha2: "MA", Alpha3: "MAR", Numeric: "504", Name: "Morocco", Currency: "MAD", DialCode: "212"}
	MC = Country{Alpha2: "MC", Alpha3: "MCO", Numeric: "492", Name: "Monaco", Currency: "EUR", DialCode: "377"}
	MD = Country{Alpha2: "MD", Alpha3: "MDA", Numeric: "498", Name: "Moldova", Currency: "MDL", DialCode: "373"}
	ME = Country{Alpha2: "ME", Alpha3: "MNE", Numeric: "499", Name: "Montenegro", Currency: "EUR", DialCode: "382"}
	MF = Country{Alpha2: "MF", Alpha3: "MAF", Numeric: "663", Name: "Saint Martin (French part)", Currency: "EUR", DialCode: "590"}
	MG = Country{Alpha2: "MG", Alpha3: "MDG", Numeric: "450", Name: "Madagascar", Currency: "MGA", DialCode: "261"}
	MH = Country{Alpha2: "MH", Alpha3: "MHL", Numeric: "584", Name: "Marshall Islands", Currency: "USD", DialCode: "692"}
	MK = Country{Alpha2: "MK", Alpha3: "MKD", Numeric: "807", Name: "North Macedonia", Currency: "MKD", DialCode: "389"}
	ML = Country{Alpha2: "ML", Alpha3: "MLI", Numeric: "466", Name: "Mali", Currency: "XOF", DialCode: "223"}
	MM = Country{Alpha2: "MM", Alpha3: "MMR", Numeric: "104", Name: "Myanmar", Currency: "MMK", DialCode: "95"}
	MN = Country{Alpha2: "MN", Alpha3: "MNG", Numeric: "496", Name: "Mongolia", Currency: "MNT", DialCode: "976"}
	MO = Country{Alpha2: "MO", Alpha3: "MAC", Numeric: "446", Name: "Macao", Currency: "MOP", DialCode: "853"}
	MP = Country{Alpha2: "MP", Alpha3: "MNP", Numeric: "580", Name: "Northern Mariana Islands", Currency: "USD", DialCode: "1"}
	MQ = Country{Alpha2: "MQ", Alpha3: "MTQ", Numeric: "474", Name: "Martinique", Currency: "EUR", DialCode: "596"}
	MR = Country{Alpha2: "MR", Alpha3: "MRT", Numeric: "478", Name: "Mauritania", Currency: "MRU", DialCode: "222"}
	MS = Country{Alpha2: "MS", Alpha3: "MSR", Numeric: "500", Name: "Montserrat", Currency: "XCD", DialCode: "1"}
	MT = Country{Alpha2: "MT", Alpha3: "MLT", Numeric: "470", Name: "Malta", Currency: "EUR", DialCode: "356"}
	MU = Country{Alpha2: "MU", Alpha3: "MUS", Numeric: "480", Name: "Mauritius", Currency: "MUR", DialCode: "230"}
	MV = Country{Alpha2: "MV", Alpha3: "MDV", Numeric: "462", Name: "Maldives", Currency: "MVR", DialCode: "960"}
	MW = Country{Alpha2: "MW", Alpha3: "MWI", Numeric: "454", Name: "Malawi", Currency: "MWK", DialCode: "265"}
	MX = Country{Alpha2: "MX", Alpha3: "MEX", Numeric: "484", Name: "Mexico", Currency: "MXN", DialCode: "52"}
	MY = Country{Alpha2: "MY", Alpha3: "MYS", Numeric: "458", Name: "Malaysia", Currency: "MYR", DialCode: "60"}
	MZ = Country{Alpha2: "MZ", Alpha3: "MOZ", Numeric: "508", Name: "Mozambique", Currency: "MZN", DialCode: "258"}
	NA = Country{Alpha2: "NA", Alpha3: "NAM", Numeric: "516", Name: "Namibia", Currency: "NAD", DialCode: "264"}
	NC = Country{Alpha2: "NC", Alpha3: "NCL", Numeric: "540", Name: "New Caledonia", Currency: "XPF", DialCode: "687"}
	NE = Country{Alpha2: "NE", Alpha3: "NER", Numeric: "562", Name: "Niger", Currency: "XOF", DialCode: "227"}
	NF = Country{Alpha2: "NF", Alpha3: "NFK", Numeric: "574", Name: "Norfolk Island", Currency: "AUD", DialCode: "672"}
	NG = Country{Alpha2: "NG", Alpha3: "NGA", Numeric: "566", Name: "Nigeria", Currency: "NGN", DialCode: "234"}
	NI = Country{Alpha2: "NI", Alpha3: "NIC", Numeric: "558", Name: "Nicaragua", Currency: "NIO", DialCode: "505"}
	NL = Country{Alpha2: "NL", Alpha3: "NLD", Numeric: "528", Name: "Netherlands", Currency: "EUR", DialCode: "31"}
	NO = Country{Alpha2: "NO", Alpha3: "NOR", Numeric: "578", Name: "Norway", Currency: "NOK", DialCode: "47"}
	NP = Country{Alpha2: "NP", Alpha3: "NPL", Numeric: "524", Name: "Nepal", Currency: "NPR", DialCode: "977"}
	NR = Country{Alpha2: "NR", Alpha3: "NRU", Numeric: "520", Name: "Nauru", Currency: "AUD", DialCode: "674"}
	NU = Country{Alpha2: "NU", Alpha3: "NIU", Numeric: "570", Name: "Niue", Currency: "NZD", DialCode: "683"}
	NZ = Country{Alpha2: "NZ", Alpha3: "NZL", Numeric: "554", Name: "New Zealand", Currency: "NZD", DialCode: "64"}
	OM = Country{Alpha2: "OM", Alpha3: "OMN", Numeric: "512", Name: "Oman", Currency: "OMR", DialCode: "968"}
	PA = Country{Alpha2: "PA", Alpha3: "PAN", Numeric: "591", Name: "Panama", Currency: "PAB", DialCode: "507"}
	PE = Country{Alpha2: "PE", Alpha3: "PER", Numeric: "604", Name: "Peru", Currency: "PEN", DialCode: "51"}
	PF = Country{Alpha2: "PF", Alpha3: "PYF", Numeric: "258", Name: "French Polynesia", Currency: "XPF", DialCode: "689"}
	PG = Country{Alpha2: "PG", Alpha3: "PNG", Numeric: "598", Name: "Papua New Guinea", Currency: "PGK", DialCode: "675"}
	PH = Country{Alpha2: "PH", Alpha3: "PHL", Numeric: "608", Name: "Philippines", Currency: "PHP", DialCode: "63"}
	PK = Country{Alpha2: "PK", Alpha3: "PAK", Numeric: "586", Name: "Pakistan", Currency: "PKR", DialCode: "92"}
	PL = Country{Alpha2: "PL", Alpha3: "POL", Numeric: "616", Name: "Poland", Currency: "PLN", DialCode: "48"}
	PM = Country{Alpha2: "PM", Alpha3: "SPM", Numeric: "666", Name: "Saint Pierre and Miquelon", Currency: "EUR", DialCode: "508"}
	PN = Country{Alpha2: "PN", Alpha3: "PCN", Numeric: "612", Name: "Pitcairn", Currency: "NZD", DialCode: "64"}
	PR = Country{Alpha2: "PR", Alpha3: "PRI", Numeric: "630", Name: "Puerto Rico", Currency: "USD", DialCode: "1"}
	PS = Country{Alpha2: "PS", Alpha3: "PSE", Numeric: "275", Name: "Palestine", Currency: "ILS", DialCode: "970"}
	PT = Country{Alpha2: "PT", Alpha3: "PRT", Numeric: "620", Name: "Portugal", Currency: "EUR", DialCode: "351"}
	PW = Country{Alpha2: "PW", Alpha3: "PLW", Numeric: "585", Name: "Palau", Currency: "USD", DialCode: "680"}
	PY = Country{Alpha2: "PY", Alpha3: "PRY", Numeric: "600", Name: "Paraguay", Currency: "PYG", DialCode: "595"}
	QA = Country{Alpha2: "QA", Alpha3: "QAT", Numeric: "634", Name: "Qatar", Currency: "QAR", DialCode: "974"}
	RE = Country{Alpha2: "RE", Alpha3: "REU", Numeric: "638", Name: "Réunion", Currency: "EUR", DialCode: "262"}
	RO = Country{Alpha2: "RO", Alpha3: "ROU", Numeric: "642", Name: "Romania", Currency: "RON", DialCode: "40"}
	RS = Country{Alpha2: "RS", Alpha3: "SRB", Numeric: "688", Name: "Serbia", Currency: "RSD", DialCode: "381"}
	RU = Country{Alpha2: "RU", Alpha3: "RUS", Numeric: "643", Name: "Russia", Currency: "RUB", DialCode: "7"}
	RW = Country{Alpha2: "RW", Alpha3: "RWA", Numeric: "646", Name: "Rwanda", Currency: "RWF", DialCode: "250"}
	SA = Country{Alpha2: "SA", Alpha3: "SAU", Numeric: "682", Name: "Saudi Arabia", Currency: "SAR", DialCode: "966"}
	SB = Country{Alpha2: "SB", Alpha3: "SLB", Numeric: "090", Name: "Solomon Islands", Currency: "SBD", DialCode: "677"}
	SC = Country{Alpha2: "SC", Alpha3: "SYC", Numeric: "690", Name: "Seychelles", Currency: "SCR", DialCode: "248"}
	SD = Country{Alpha2: "SD", Alpha3: "SDN", Numeric: "729", Name: "Sudan", Currency: "SDG", DialCode: "249"}
	SE = Country{Alpha2: "SE", Alpha3: "SWE", Numeric: "752", Name: "Sweden", Currency: "SEK", DialCode: "46"}
	SG = Country{Alpha2: "SG", Alpha3: "SGP", Numeric: "702", Name: "Singapore", Currency: "SGD", DialCode: "65"}
	SH = Country{Alpha2: "SH", Alpha3: "SHN", Numeric: "654", Name: "Saint Helena, Ascension and Tristan da Cunha", Currency: "SHP", DialCode: "290"}
	SI = Country{Alpha2: "SI", Alpha3: "SVN", Numeric: "705", Name: "Slovenia", Currency: "EUR", DialCode: "386"}
	SJ = Country{Alpha2: "SJ", Alpha3: "SJM", Numeric: "744", Name: "Svalbard and Jan Mayen", Currency: "NOK", DialCode: "47"}
	SK = Country{Alpha2: "SK", Alpha3: "SVK", Numeric: "703", Name: "Slovakia", Currency: "EUR", DialCode: "421"}
	SL = Country{Alpha2: "SL", Alpha3: "SLE", Numeric: "694", Name: "Sierra Leone", Currency: "SLE", DialCode: "232"}
	SM = Country{Alpha2: "SM", Alpha3: "SMR", Numeric: "674", Name: "San Marino", Currency: "EUR", DialCode: "378"}
	SN = Country{Alpha2: "SN", Alpha3: "SEN", Numeric: "686", Name: "Senegal", Currency: "XOF", DialCode: "221"}
	SO = Country{Alpha2: "SO", Alpha3: "SOM", Numeric: "706", Name: "Somalia", Currency: "SOS", DialCode: "252"}
	SR = Country{Alpha2: "SR", Alpha3: "SUR", Numeric: "740", Name: "Suriname", Currency: "SRD", DialCode: "597"}
	SS = Country{Alpha2: "SS", Alpha3: "SSD", Numeric: "728", Name: "South Sudan", Currency: "SSP", DialCode: "211"}
	ST = Country{Alpha2: "ST", Alpha3: "STP", Numeric: "678", Name: "Sao Tome and Principe", Currency: "STN", DialCode: "239"}
	SV = Country{Alpha2: "SV", Alpha3: "SLV", Numeric: "222", Name: "El Salvador", Currency: "USD", DialCode: "503"}
	SX = Country{Alpha2: "SX", Alpha3: "SXM", Numeric: "534", Name: "Sint Maarten (Dutch part)", Currency: "ANG", DialCode: "1"}
	SY = Country{Alpha2: "SY", Alpha3: "SYR", Numeric: "760", Name: "Syria", Currency: "SYP", DialCode: "963"}
	SZ = Country{Alpha2: "SZ", Alpha3: "SWZ", Numeric: "748", Name: "Eswatini", Currency: "SZL", DialCode: "268"}
	TC = Country{Alpha2: "TC", Alpha3: "TCA", Numeric: "796", Name: "Turks and Caicos Islands", Currency: "USD", DialCode: "1"}
	TD = Country{Alpha2: "TD", Alpha3: "TCD", Numeric: "148", Name: "Chad", Currency: "XAF", DialCode: "235"}
	TF = Country{Alpha2: "TF", Alpha3: "ATF", Numeric: "260", Name: "French Southern Territories", Currency: "EUR", DialCode: "262"}
	TG = Country{Alpha2: "TG", Alpha3: "TGO", Numeric: "768", Name: "Togo", Currency: "XOF", DialCode: "228"}
	TH = Country{Alpha2: "TH", Alpha3: "THA", Numeric: "764", Name: "Thailand", Currency: "THB", DialCode: "66"}
	TJ = Country{Alpha2: "TJ", Alpha3: "TJK", Numeric: "762", Name: "Tajikistan", Currency: "TJS", DialCode: "992"}
	TK = Country{Alpha2: "TK", Alpha3: "TKL", Numeric: "772", Name: "Tokelau", Currency: "NZD", DialCode: "690"}
	TL = Country{Alpha2: "TL", Alpha3: "TLS", Numeric: "626", Name: "Timor-Leste", Currency: "USD", DialCode: "670"}
	TM = Country{Alpha2: "TM", Alpha3: "TKM", Numeric: "795", Name: "Turkmenistan", Currency: "TMT", DialCode: "993"}
	TN = Country{Alpha2: "TN", Alpha3: "TUN", Numeric: "788", Name: "Tunisia", Currency: "TND", DialCode: "216"}
	TO = Country{Alpha2: "TO", Alpha3: "TON", Numeric: "776", Name: "Tonga", Currency: "TOP", DialCode: "676"}
	TR = Country{Alpha2: "TR", Alpha3: "TUR", Numeric: "792", Name: "Türkiye", Currency: "TRY", DialCode: "90"}
	TT = Country{Alpha2: "TT", Alpha3: "TTO", Numeric: "780", Name: "Trinidad and Tobago", Currency: "TTD", DialCode: "1"}
	TV = Country{Alpha2: "TV", Alpha3: "TUV", Numeric: "798", Name: "Tuvalu", Currency: "AUD", DialCode: "688"}
	TW = Country{Alpha2: "TW", Alpha3: "TWN", Numeric: "158", Name: "Taiwan", Currency: "TWD", DialCode: "886"}
	TZ = Country{Alpha2: "TZ", Alpha3: "TZA", Numeric: "834", Name: "Tanzania", Currency: "TZS", DialCode: "255"}
	UA = Country{Alpha2: "UA", Alpha3: "UKR", Numeric: "804", Name: "Ukraine", Currency: "UAH", DialCode: "380"}
	UG = Country{Alpha2: "UG", Alpha3: "UGA", Numeric: "800", Name: "Uganda", Currency: "UGX", DialCode: "256"}
	UM = Country{Alpha2: "UM", Alpha3: "UMI", Numeric: "581", Name: "United States Minor Outlying Islands", Currency: "USD", DialCode: "1"}
	US = Country{Alpha2: "US", Alpha3: "USA", Numeric: "840", Name: "United States", Currency: "USD", DialCode: "1"}
	UY = Country{Alpha2: "UY", Alpha3: "URY", Numeric: "858", Name: "Uruguay", Currency: "UYU", DialCode: "598"}
	UZ = Country{Alpha2: "UZ", Alpha3: "UZB", Numeric: "860", Name: "Uzbekistan", Currency: "UZS", DialCode: "998"}
	VA = Country{Alpha2: "VA", Alpha3: "VAT", Numeric: "336", Name: "Holy See", Currency: "EUR", DialCode: "379"}
	VC = Country{Alpha2: "VC", Alpha3: "VCT", Numeric: "670", Name: "Saint Vincent and the Grenadines", Currency: "XCD", DialCode: "1"}
	VE = Country{Alpha2: "VE", Alpha3: "VEN", Numeric: "862", Name: "Venezuela", Currency: "VES", DialCode: "58"}
	VG = Country{Alpha2: "VG", Alpha3: "VGB", Numeric: "092", Name: "Virgin Islands (British)", Currency: "USD", DialCode: "1"}
	VI = Country{Alpha2: "VI", Alpha3: "VIR", Numeric: "850", Name: "Virgin Islands (U.S.)", Currency: "USD", DialCode: "1"}
	VN = Country{Alpha2: "VN", Alpha3: "VNM", Numeric: "704", Name: "Vietnam", Currency: "VND", DialCode: "84"}
	VU = Country{Alpha2: "VU", Alpha3: "VUT", Numeric: "548", Name: "Vanuatu", Currency: "VUV", DialCode: "678"}
	WF = Country{Alpha2: "WF", Alpha3: "WLF", Numeric: "876", Name: "Wallis and Futuna", Currency: "XPF", DialCode: "681"}
	WS = Country{Alpha2: "WS", Alpha3: "WSM", Numeric: "882", Name: "Samoa", Currency: "WST", DialCode: "685"}
	YE = Country{Alpha2: "YE", Alpha3: "YEM", Numeric: "887", Name: "Yemen", Currency: "YER", DialCode: "967"}
	YT = Country{Alpha2: "YT", Alpha3: "MYT", Numeric: "175", Name: "Mayotte", Currency: "EUR", DialCode: "262"}
	ZA = Country{Alpha2: "ZA", Alpha3: "ZAF", Numeric: "710", Name: "South Africa", Currency: "ZAR", DialCode: "27"}
	ZM = Country{Alpha2: "ZM", Alpha3: "ZMB", Numeric: "894", Name: "Zambia", Currency: "ZMW", DialCode: "260"}
	ZW = Country{Alpha2: "ZW", Alpha3: "ZWE", Numeric: "716", Name: "Zimbabwe", Currency: "ZWG", DialCode: "263"}
)

// all is the bundled table as pointers into the exported vars, so the init emoji
// fill lands on the vars themselves. Full ISO-3166-1 set, alpha-2 ordered.
var all = []*Country{
	&AD, &AE, &AF, &AG, &AI, &AL, &AM, &AO, &AQ, &AR, &AS, &AT, &AU, &AW, &AX, &AZ,
	&BA, &BB, &BD, &BE, &BF, &BG, &BH, &BI, &BJ, &BL, &BM, &BN, &BO, &BQ, &BR, &BS, &BT, &BV, &BW, &BY, &BZ,
	&CA, &CC, &CD, &CF, &CG, &CH, &CI, &CK, &CL, &CM, &CN, &CO, &CR, &CU, &CV, &CW, &CX, &CY, &CZ,
	&DE, &DJ, &DK, &DM, &DO, &DZ,
	&EC, &EE, &EG, &EH, &ER, &ES, &ET,
	&FI, &FJ, &FK, &FM, &FO, &FR,
	&GA, &GB, &GD, &GE, &GF, &GG, &GH, &GI, &GL, &GM, &GN, &GP, &GQ, &GR, &GS, &GT, &GU, &GW, &GY,
	&HK, &HM, &HN, &HR, &HT, &HU,
	&ID, &IE, &IL, &IM, &IN, &IO, &IQ, &IR, &IS, &IT,
	&JE, &JM, &JO, &JP,
	&KE, &KG, &KH, &KI, &KM, &KN, &KP, &KR, &KW, &KY, &KZ,
	&LA, &LB, &LC, &LI, &LK, &LR, &LS, &LT, &LU, &LV, &LY,
	&MA, &MC, &MD, &ME, &MF, &MG, &MH, &MK, &ML, &MM, &MN, &MO, &MP, &MQ, &MR, &MS, &MT, &MU, &MV, &MW, &MX, &MY, &MZ,
	&NA, &NC, &NE, &NF, &NG, &NI, &NL, &NO, &NP, &NR, &NU, &NZ,
	&OM,
	&PA, &PE, &PF, &PG, &PH, &PK, &PL, &PM, &PN, &PR, &PS, &PT, &PW, &PY,
	&QA,
	&RE, &RO, &RS, &RU, &RW,
	&SA, &SB, &SC, &SD, &SE, &SG, &SH, &SI, &SJ, &SK, &SL, &SM, &SN, &SO, &SR, &SS, &ST, &SV, &SX, &SY, &SZ,
	&TC, &TD, &TF, &TG, &TH, &TJ, &TK, &TL, &TM, &TN, &TO, &TR, &TT, &TV, &TW, &TZ,
	&UA, &UG, &UM, &US, &UY, &UZ,
	&VA, &VC, &VE, &VG, &VI, &VN, &VU,
	&WF, &WS,
	&YE, &YT,
	&ZA, &ZM, &ZW,
}
