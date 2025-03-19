

# Will be removed
class AllFeature:
    name = "AllFeature"
    parent = "AllFeatures"
    
    @staticmethod
    def extract_feature(json_entry: dict):
        return json_entry


# Will be removed
class NoneFeature:
    name = "NoneFeature"
    parent = "NoneFeatures"
    
    @staticmethod
    def extract_feature(json_entry):
        return {}
    

FEATURE_LIST = [AllFeature, NoneFeature]
