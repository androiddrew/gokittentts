# Issue 19 review sheet: normalizer deviations

There are 108 overrides from `testdata/normalize_overrides.yaml`, grouped by reason. For each group, compare **Python** (what the reference does) with **Go** (what gokittentts does now), then set its **Decision** to one of:

- `approve`: keep the Go behavior. Every entry in the group gets `approved: true`.
- `edit: <what to change>`: change the Go behavior, then approve the new output.
- `revert`: make Go match Python and remove the entries. Python's crashes can't be reverted literally.

**Outcome (2026-10-01):** the owner approved all 108 entries as they are, G11 and G17 included, with no edits or reverts. The agent's notes below are what was raised during the review. A Python cell that reads `ValueError: …` or `KeyError: …` means Python crashes on that input.


## G1: 14 entries

Python's number pattern takes the commas after a number, so the pause after it is lost. The commas stay.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| There were 3, maybe 4. | There were three maybe four. | There were three, maybe four. |
| I picked 5, 6 and 7. | I picked five six and seven. | I picked five, six and seven. |
| Steps 1, 2, and 3 are done. | Steps one two and three are done. | Steps one, two, and three are done. |
| In 2024, the team grew. | In twenty twenty-four the team grew. | In twenty twenty-four, the team grew. |
| By 1999, the web was everywhere. | By nineteen ninety-nine the web was everywhere. | By nineteen ninety-nine, the web was everywhere. |
| After 10, we stopped. | After ten we stopped. | After ten, we stopped. |
| Sizes: 8, 10, 12. | Sizes: eight ten twelve. | Sizes: eight, ten, twelve. |
| The answer, 42, was unexpected. | The answer, forty-two was unexpected. | The answer, forty-two, was unexpected. |
| May 5, 20261 is not a date. | May five twenty thousand two hundred sixty-one is not a date. | May five, twenty thousand two hundred sixty-one is not a date. |
| Mayday 5, 2020 is not a date. | Mayday five twenty twenty is not a date. | Mayday five, twenty twenty is not a date. |
| XMay 5, 2020 again. | XMay five twenty twenty again. | XMay five, twenty twenty again. |
| Coordinates -5,-6 here. | Coordinates negative fivenegative six here. | Coordinates negative five,negative six here. |
| Pay $1,000, then leave. | Pay one thousand dollars then leave. | Pay one thousand dollars, then leave. |
| Smith et al. 2024, pp. 31-35 | Smith et al twenty twenty-four pages thirty-one to thirty-five | Smith et al twenty twenty-four, pages thirty-one to thirty-five |

## G2: 5 entries

Python raises ValueError: its number pattern matches a lone comma after a non-letter and calls int(""). The comma is left alone.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| The code (404), not found. | ValueError: invalid literal for int() with base 10: '' | The code four hundred four , not found. |
| He said "hi", then left. | ValueError: invalid literal for int() with base 10: '' | He said hi , then left. |
| Wait , what? | ValueError: invalid literal for int() with base 10: '' | Wait , what? |
| "Yes", she said. | ValueError: invalid literal for int() with base 10: '' | Yes , she said. |
| Item (a), item (b). | ValueError: invalid literal for int() with base 10: '' | Item a , item b . |

## G3: 4 entries

Python drops the digits above the trillions, so long numbers are misread or deleted. A number over 15 digits is read digit by digit.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| The debt is 1,000,000,000,000,000 dollars. | The debt is dollars. | The debt is one zero zero zero zero zero zero zero zero zero zero zero zero zero zero zero dollars. |
| Avogadro's number is about 602214076000000000000000. | Avogadro s number is about . | Avogadro s number is about six zero two two one four zero seven six zero zero zero zero zero zero zero zero zero zero zero zero zero zero zero. |
| That serial is 12345678901234567890. | That serial is six hundred seventy-eight trillion nine hundred one billion two hundred thirty-four million five hundred sixty-seven thousand eight hundred ninety. | That serial is one two three four five six seven eight nine zero one two three four five six seven eight nine zero. |
| The 1000000000000000000th try. | The th try. | The one zero zero zero zero zero zero zero zero zero zero zero zero zero zero zero zero zero zeroth try. |

## G4: 8 entries

Python makes ordinals of the tens "-yth" ("twentyth"). They end in "-ieth" ("twentieth").

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| It is our 20th year. | It is our twentyth year. | It is our twentieth year. |
| The 30th reunion was fun. | The thirtyth reunion was fun. | The thirtieth reunion was fun. |
| The 40th, 50th and 60th games. | The fortyth, fiftyth and sixtyth games. | The fortieth, fiftieth and sixtieth games. |
| The 90th minute. | The ninetyth minute. | The ninetieth minute. |
| Apr. 30, 1999 was the last day. | April thirtyth, nineteen ninety-nine was the last day. | April thirtieth, nineteen ninety-nine was the last day. |
| Jul. 20, 1969 was the moon landing. | July twentyth, nineteen sixty-nine was the moon landing. | July twentieth, nineteen sixty-nine was the moon landing. |
| Sep 30, 2019 | September thirtyth, twenty nineteen | September thirtieth, twenty nineteen |
| The score was 3-1 at the 90th minute. | The score was three to one at the ninetyth minute. | The score was three to one at the ninetieth minute. |

## G5: 6 entries

Python takes the spaces after a time with no am or pm, joining it to the next word ("threetoday"). The spaces stay.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| The call is at 10:00 tomorrow. | The call is at tentomorrow. | The call is at ten tomorrow. |
| We arrive at 3:00 today. | We arrive at threetoday. | We arrive at three today. |
| Scores were 21:14 at halftime. | Scores were twenty-one fourteenat halftime. | Scores were twenty-one fourteen at halftime. |
| Run the job every 15 minutes between 9:00 and 17:00. | Run the job every fifteen minutes between nineand seventeen. | Run the job every fifteen minutes between nine and seventeen. |
| At 3:05 ammo. | At three oh fiveammo. | At three oh five ammo. |
| Arabic time ٣:٣٠ here. | Arabic time three thirtyhere. | Arabic time three thirty here. |

## G6: 1 entry

Python makes ordinals of the tens "-yth" ("twentyth"). They end in "-ieth" ("twentieth"). Python takes the spaces after a time with no am or pm, joining it to the next word ("threetoday"). The spaces stay.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| The Apollo 11 mission landed on July 20, 1969 at 20:17 UTC. | The Apollo eleven mission landed on July twentyth, nineteen sixty-nine at twenty seventeenUTC. | The Apollo eleven mission landed on July twentieth, nineteen sixty-nine at twenty seventeen UTC. |

## G7: 1 entry

Python's month pattern matches "ſept" case-insensitively but its lower() doesn't fold the long s, so the month lookup raises KeyError. The month is looked up case-folded.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| The ſept 5, 2020 launch. | KeyError: 'ſept' | The September fifth, twenty twenty launch. |

## G8: 5 entries

Python reads an amount followed by a scale word as "three dollars and fifty cents million". The scale word goes before the unit: "three point five million dollars".

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| We raised $3.5 million. | We raised three dollars and fifty cents million. | We raised three point five million dollars. |
| The budget was $3.5 million. | The budget was three dollars and fifty cents million. | The budget was three point five million dollars. |
| We need $2 billion. | We need two dollars billion. | We need two billion dollars. |
| It costs $3.5 Million. | It costs three dollars and fifty cents Million. | It costs three point five million dollars. |
| Raise $10 thousand now. | Raise ten dollars thousand now. | Raise ten thousand dollars now. |

## G9: 2 entries

Python adds an "s" to yen and won. They are invariant.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| A coffee is ¥500. | A coffee is five hundred yens. | A coffee is five hundred yen. |
| The ₩5000 note. | The five thousand wons note. | The five thousand won note. |

## G10: 28 entries

Python leaves units after a number as written ("three GB"). Units are spelled out, ported from TextPreprocessor's expand_units, singular after 1.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| 3 GB | three GB | three gigabytes |
| It has 16 GB of memory. | It has sixteen GB of memory. | It has sixteen gigabytes of memory. |
| The file is 500 MB. | The file is five hundred MB. | The file is five hundred megabytes. |
| Drive 60 mph. | Drive sixty mph. | Drive sixty miles per hour. |
| It is 25 °C outside. | It is twenty-five C outside. | It is twenty-five degrees Celsius outside. |
| The CPU runs at 3.5 GHz. | The CPU runs at three point five GHz. | The CPU runs at three point five gigahertz. |
| Latency was 20 ms. | Latency was twenty Ms | Latency was twenty milliseconds. |
| The bag weighs 5 kg. | The bag weighs five kg. | The bag weighs five kilograms. |
| It is 10 km away. | It is ten km away. | It is ten kilometers away. |
| Take 200 mg twice a day. | Take two hundred mg twice a day. | Take two hundred milligrams twice a day. |
| Add 250 ml of water. | Add two hundred fifty ml of water. | Add two hundred fifty milliliters of water. |
| Non-breaking 5 kg. | Non-breaking five kg. | Non-breaking five kilograms. |
| 1 km is short. | one km is short. | one kilometer is short. |
| It is 100km away. | It is one hundredkm away. | It is one hundred kilometers away. |
| It weighs 50kg. | It weighs fiftykg. | It weighs fifty kilograms. |
| Set 25°C please. | Set twenty-five C please. | Set twenty-five degrees Celsius please. |
| Set 25 °F please. | Set twenty-five F please. | Set twenty-five degrees Fahrenheit please. |
| Use 5GB of space. | Use fiveGB of space. | Use five gigabytes of space. |
| 1.5 TB of disk. | one point five TB of disk. | one point five terabytes of disk. |
| Plays at 44.1 kHz. | Plays at forty-four point one kHz. | Plays at forty-four point one kilohertz. |
| A 2.4GHz band. | A two point fourGHz band. | A two point four gigahertz band. |
| Wait 5 ns now. | Wait five ns now. | Wait five nanoseconds now. |
| Wait 3 µs now. | Wait three µs now. | Wait three microseconds now. |
| Runs 5-10 km a day. | Runs five to ten km a day. | Runs five to ten kilometers a day. |
| Delay of 1 ms only. | Delay of one ms only. | Delay of one millisecond only. |
| Speed 1 mph. | Speed one mph. | Speed one mile per hour. |
| Temp 25C° today. | Temp twenty-fiveC today. | Temp twenty-five degrees Celsius today. |
| Only 01 km left. | Only one km left. | Only one kilometer left. |

## G11: 7 entries

Python leaves a scale letter after a number as written ("sevenB"). It is read as the scale word, ported from TextPreprocessor's expand_scale_suffixes.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** The expansion happens wherever the letter appears: "Room 4B" reads "four billion" and "A 4K screen" reads "four thousand screen". Options: approve as is, exempt 4K/8K, or revert to Python ("sevenB", which espeak-ng reads unpredictably).

| Input | Python | Go |
| --- | --- | --- |
| The model has 7B parameters. | The model has sevenB parameters. | The model has seven billion parameters. |
| We served 10K users. | We served tenK users. | We served ten thousand users. |
| The 340M model. | The three hundred fortyM model. | The three hundred forty million model. |
| A 1.5K salary. | A one point fiveK salary. | A one point five thousand salary. |
| About 2T tokens. | About twoT tokens. | About two trillion tokens. |
| A 4K screen. | A fourK screen. | A four thousand screen. |
| Plan 9B is weird. | Plan nineB is weird. | Plan nine billion is weird. |

## G12: 1 entry

Python reads an amount followed by a scale word as "three dollars and fifty cents million". The scale word goes before the unit: "three point five million dollars". Python's number pattern takes the commas after a number, so the pause after it is lost. The commas stay.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| In 2023, revenue grew 15% to $4.2 billion, up from $3.6 billion in 2022. | In twenty twenty-three revenue grew fifteen percent to four dollars and twenty cents billion, up from three dollars and sixty cents billion in twenty twenty-two. | In twenty twenty-three, revenue grew fifteen percent to four point two billion dollars, up from three point six billion dollars in twenty twenty-two. |

## G13: 2 entries

Python says "one dollars" when there are cents. One whole unit is singular.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| $1.50 each. | one dollars and fifty cents each. | one dollar and fifty cents each. |
| Price: $1.01. | Price: one dollars and one cent. | Price: one dollar and one cent. |

## G14: 1 entry

Python raises ValueError on a currency symbol with only commas after it. It is left alone.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| A $, sign. | ValueError: invalid literal for int() with base 10: '' | A , sign. |

## G15: 2 entries

Python reads a percent through float(), and raises ValueError when the float prints in exponent form. The decimal is read as written.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| Down 0.00001% only. | ValueError: invalid literal for int() with base 10: '1e-05' | Down zero point zero zero zero zero one percent only. |
| Up 12345678901234567.5% today. | ValueError: invalid literal for int() with base 10: 'e' | Up one two three four five six seven eight nine zero one two three four five six seven point five percent today. |

## G16: 4 entries

Python raises ValueError on a model version ending in or doubling a dot ("GPT-4." at the end of a sentence). The digits-and-dots version is read and the rest stays.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| GPT-4. | ValueError: invalid literal for int() with base 10: '' | GPT four. |
| I love GPT-4. It is great. | ValueError: invalid literal for int() with base 10: '' | I love GPT four. It is great. |
| Use gpt-3.5. Then stop. | ValueError: invalid literal for int() with base 10: '' | Use gpt three point five. Then stop. |
| Model-3..5 is odd. | ValueError: invalid literal for int() with base 10: '' | Model three..five is odd. |

## G17: 2 entries

Python's percent pattern takes a minus after a word character as a sign ("5-10%" is "fivenegative ten percent"). It stays a hyphen.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** This is inconsistent with other ranges: "5-10 km" reads "five to ten kilometers". Suggest changing Go so that "5-10%" reads "five to ten percent".

| Input | Python | Go |
| --- | --- | --- |
| x-5% here. | xnegative five percent here. | x-five percent here. |
| Get 5-10% off. | Get fivenegative ten percent off. | Get five-ten percent off. |

## G18: 2 entries

Python spells its own "www dot" as "w w w d o t". It is "w w w dot".

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| See www.example.org for details. | See w w w d o t e x a m p l e dot o r g for details. | See w w w dot e x a m p l e dot o r g for details. |
| Try www.Example.com/a_b now. | Try w w w d o t e x a m p l e dot c o m slash a underscore b now. | Try w w w dot e x a m p l e dot c o m slash a underscore b now. |

## G19: 4 entries

Python spells the punctuation ending a URL ("... c o m dot"), so the sentence or clause loses its ending. It stays in the text; closing brackets and quotes there are dropped.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| The repo is at https://github.com/KittenML/KittenTTS. | The repo is at g i t h u b dot c o m slash k i t t e n m l slash k i t t e n t t s dot | The repo is at g i t h u b dot c o m slash k i t t e n m l slash k i t t e n t t s. |
| See (https://example.com). | See e x a m p l e dot c o m dot | See e x a m p l e dot c o m. |
| Visit https://example.com, then go. | Visit e x a m p l e dot c o m then go. | Visit e x a m p l e dot c o m, then go. |
| Have you seen https://a.com/x? | Have you seen a dot c o m slash x question mark | Have you seen a dot c o m slash x? |

## G20: 2 entries

Python matches URLs case-sensitively, so "HTTPS://EXAMPLE.COM" is read as "HTTPS: EXAMPLE.COM". URLs match case-insensitively.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| Visit HTTPS://EXAMPLE.COM now. | Visit HTTPS: EXAMPLE.COM now. | Visit e x a m p l e dot c o m now. |
| Go to WWW.EXAMPLE.COM now. | Go to WWW.EXAMPLE.COM now. | Go to w w w dot e x a m p l e dot c o m now. |

## G21: 1 entry

Python raises KeyError spelling a non-ASCII digit in a URL. The digit is read.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| Go to https://x.com/٣ now. | KeyError: '٣' | Go to x dot c o m slash three now. |

## G22: 2 entries

Python's email pattern stops at the domain's first dot ("a at b dot c o.uk"). A domain can have several dots.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| Mail a@b.co.uk now. | Mail a at b dot c o.uk now. | Mail a at b dot c o dot u k now. |
| Write to first.last+tag@sub-domain.example.org today. | Write to f i r s t dot l a s t t a g at s u b dash d o m a i n dot e x a m p l e.org today. | Write to f i r s t dot l a s t t a g at s u b dash d o m a i n dot e x a m p l e dot o r g today. |

## G23: 3 entries

Python reads the "p." of "p.m." as the title "page" ("five pagem."). "p.m." is left alone.

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| Meet at 5 p.m. today. | Meet at five pagem. today. | Meet at five p.m. today. |
| At 5 P.M. sharp. | At five pageM. sharp. | At five P.M. sharp. |
| Meet at 5 p.m, ok. | Meet at five pagem, ok. | Meet at five p.m, ok. |

## G24: 1 entry

Python's HTML pattern takes "< 5 and 6 >" for a tag and deletes it. A tag needs a name after "<".

**Decision:** approved by the owner, 2026-10-01  
**Agent's note:** A fix over Python.

| Input | Python | Go |
| --- | --- | --- |
| Use 3 < 5 and 6 > 2. | Use three two. | Use three five and six two. |
